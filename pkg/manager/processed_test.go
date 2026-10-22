/*
Copyright 2026 Nscale.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

package manager_test

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"go.uber.org/mock/gomock"

	unikornv1 "github.com/unikorn-cloud/core/pkg/apis/unikorn/v1alpha1"
	unikornv1fake "github.com/unikorn-cloud/core/pkg/apis/unikorn/v1alpha1/fake"
	"github.com/unikorn-cloud/core/pkg/constants"
	"github.com/unikorn-cloud/core/pkg/manager"
	"github.com/unikorn-cloud/core/pkg/provisioners"
	mockprovisioners "github.com/unikorn-cloud/core/pkg/provisioners/mock"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/interceptor"
)

const (
	// priorGeneration is the generation last finished with.
	priorGeneration = 1

	// currentGeneration is the spec generation the pass runs against.
	currentGeneration = 2
)

// reconcileGenerational runs one pass that returns perr against a resource
// last finished at priorGeneration, and returns what its status writes record.
// It reads the writes, not the stored object, because a successful
// deprovision deletes the object.
func reconcileGenerational(t *testing.T, perr error, polling, deleting bool) int64 {
	t.Helper()

	c := gomock.NewController(t)
	defer c.Finish()

	request := &unikornv1fake.GenerationalResource{
		ObjectMeta: metav1.ObjectMeta{
			Namespace:  testNamespace,
			Name:       testName,
			Generation: currentGeneration,
		},
	}

	request.SetProcessedGeneration(priorGeneration)

	if deleting {
		request.Finalizers = []string{constants.Finalizer}
		request.DeletionTimestamp = &metav1.Time{Time: time.Now()}
	}

	recorded := int64(priorGeneration)

	funcs := interceptor.Funcs{
		SubResourceUpdate: func(ctx context.Context, cli client.Client, subResource string, object client.Object, opts ...client.SubResourceUpdateOption) error {
			if processor, ok := object.(unikornv1.GenerationProcessor); ok {
				recorded = processor.ProcessedGeneration()
			}

			return cli.SubResource(subResource).Update(ctx, object, opts...)
		},
	}

	tc := mustNewTestContextWithInterceptor(t, funcs, request)

	pass := func(_ context.Context) error { return perr }

	p := mockprovisioners.NewMockManagerProvisioner(c)
	p.EXPECT().Object().Return(&unikornv1fake.GenerationalResource{})

	if deleting {
		p.EXPECT().Deprovision(gomock.Any()).DoAndReturn(pass)
	} else {
		p.EXPECT().Provision(gomock.Any()).DoAndReturn(pass)
	}

	opts := managerOptions()

	var reconcilerOptions []manager.ReconcilerOption

	if polling {
		opts = managerOptionsWithRequeuePeriod()

		reconcilerOptions = append(reconcilerOptions, manager.WithPolling())
	}

	reconciler := manager.NewReconciler(opts, nil, tc.newManager(c), func(_ manager.ControllerOptions) provisioners.ManagerProvisioner { return p }, reconcilerOptions...)

	_, _ = reconciler.Reconcile(t.Context(), newRequest(testNamespace, testName))

	return recorded
}

// TestProcessedGenerationStampsOnlySuccess checks only a successful provision
// records the generation as finished with.  Every other outcome must leave the
// last good stamp alone, or a provisioner reading it would take a spec that
// never applied for one that did.
func TestProcessedGenerationStampsOnlySuccess(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		err      error
		polling  bool
		deleting bool
		want     int64
	}{
		{name: "Success", want: currentGeneration},
		{name: "SuccessPolling", polling: true, want: currentGeneration},
		{name: "Yield", err: provisioners.ErrYield, want: priorGeneration},
		{name: "BareRecheck", err: provisioners.ErrRecheck, polling: true, want: priorGeneration},
		{name: "TypedRecheck", err: provisioners.Recheck(unikornv1.ConditionReasonErrored, "the server is in error"), polling: true, want: priorGeneration},
		{name: "Terminal", err: provisioners.ErrTerminal, want: priorGeneration},
		{name: "Unexpected", err: errUnhandled, want: priorGeneration},
		{name: "Canceled", err: context.Canceled, want: priorGeneration},
		{name: "Deprovision", deleting: true, want: priorGeneration},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()

			assert.Equal(t, test.want, reconcileGenerational(t, test.err, test.polling, test.deleting))
		})
	}
}
