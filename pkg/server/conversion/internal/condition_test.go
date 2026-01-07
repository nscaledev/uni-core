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

//nolint:testpackage
package internal

import (
	"testing"

	"github.com/stretchr/testify/require"
	"go.uber.org/mock/gomock"

	unikornv1 "github.com/unikorn-cloud/core/pkg/apis/unikorn/v1alpha1"
	mockv1 "github.com/unikorn-cloud/core/pkg/apis/unikorn/v1alpha1/mock"
	"github.com/unikorn-cloud/core/pkg/openapi"
	"github.com/unikorn-cloud/core/pkg/util/testutil"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

type FakeConditionObject struct {
	metav1.ObjectMeta
	*mockv1.MockStatusConditionReader
}

func NewFakeConditionObject(t *testing.T) *FakeConditionObject {
	t.Helper()

	mockController := gomock.NewController(t)
	t.Cleanup(mockController.Finish)

	mockReader := mockv1.NewMockStatusConditionReader(mockController)

	return &FakeConditionObject{
		MockStatusConditionReader: mockReader,
	}
}

func (o *FakeConditionObject) SetDeleteTimestamp() *FakeConditionObject {
	o.DeletionTimestamp = &metav1.Time{}
	return o
}

func (o *FakeConditionObject) ExpectStatusConditionRead(conditionType unikornv1.ConditionType, condition *unikornv1.Condition, err error) *FakeConditionObject {
	o.MockStatusConditionReader.
		EXPECT().
		StatusConditionRead(conditionType).
		Return(condition, err)

	return o
}

func TestConvertStatusCondition(t *testing.T) {
	t.Parallel()

	type TestCase struct {
		Name       string
		ObjectFunc func(t *testing.T) metav1.Object
		Expected   openapi.ResourceProvisioningStatus
	}

	successCase := func(name string, reason unikornv1.ConditionReason, expected openapi.ResourceProvisioningStatus) TestCase {
		return TestCase{
			Name: name,
			ObjectFunc: func(t *testing.T) metav1.Object {
				t.Helper()

				return NewFakeConditionObject(t).ExpectStatusConditionRead(
					unikornv1.ConditionAvailable,
					&unikornv1.Condition{Reason: reason},
					nil,
				)
			},
			Expected: expected,
		}
	}

	testCases := []TestCase{
		{
			Name: "returns status deprovisioning when deletion timestamp is set",
			ObjectFunc: func(t *testing.T) metav1.Object {
				t.Helper()
				return NewFakeConditionObject(t).SetDeleteTimestamp()
			},
			Expected: openapi.ResourceProvisioningStatusDeprovisioning,
		},
		{
			Name: "returns status provisioned when object does not implement StatusConditionReader",
			ObjectFunc: func(t *testing.T) metav1.Object {
				t.Helper()
				return &metav1.ObjectMeta{}
			},
			Expected: openapi.ResourceProvisioningStatusProvisioned,
		},
		{
			Name: "returns status unknown when condition read fails",
			ObjectFunc: func(t *testing.T) metav1.Object {
				t.Helper()

				return NewFakeConditionObject(t).ExpectStatusConditionRead(
					unikornv1.ConditionAvailable,
					nil,
					testutil.ErrMustFail,
				)
			},
			Expected: openapi.ResourceProvisioningStatusUnknown,
		},
		successCase(
			"returns status provisioning when condition reason is provisioning",
			unikornv1.ConditionReasonProvisioning,
			openapi.ResourceProvisioningStatusProvisioning,
		),
		successCase(
			"returns status provisioned when condition reason is provisioned",
			unikornv1.ConditionReasonProvisioned,
			openapi.ResourceProvisioningStatusProvisioned,
		),
		successCase(
			"returns status error when condition reason is errored",
			unikornv1.ConditionReasonErrored,
			openapi.ResourceProvisioningStatusError,
		),
		successCase(
			"returns status deprovisioning when condition reason is deprovisioning",
			unikornv1.ConditionReasonDeprovisioning,
			openapi.ResourceProvisioningStatusDeprovisioning,
		),
		successCase(
			"returns status unknown when condition reason is unrecognized",
			testutil.Gibberish,
			openapi.ResourceProvisioningStatusUnknown,
		),
	}

	for _, testCase := range testCases {
		t.Run(testCase.Name, func(t *testing.T) {
			t.Parallel()

			object := testCase.ObjectFunc(t)

			status := ConvertStatusCondition(object)
			require.Equal(t, testCase.Expected, status)
		})
	}
}

func TestConvertHealthCondition(t *testing.T) {
	t.Parallel()

	type TestCase struct {
		Name       string
		ObjectFunc func(t *testing.T) any
		Expected   openapi.ResourceHealthStatus
	}

	successCase := func(name string, reason unikornv1.ConditionReason, expected openapi.ResourceHealthStatus) TestCase {
		return TestCase{
			Name: name,
			ObjectFunc: func(t *testing.T) any {
				t.Helper()

				return NewFakeConditionObject(t).ExpectStatusConditionRead(
					unikornv1.ConditionHealthy,
					&unikornv1.Condition{Reason: reason},
					nil,
				)
			},
			Expected: expected,
		}
	}

	testCases := []TestCase{
		{
			Name: "returns status healthy when object does not implement StatusConditionReader",
			ObjectFunc: func(t *testing.T) any {
				t.Helper()
				return &metav1.ObjectMeta{}
			},
			Expected: openapi.ResourceHealthStatusHealthy,
		},
		{
			Name: "returns status unknown when condition read fails",
			ObjectFunc: func(t *testing.T) any {
				t.Helper()

				return NewFakeConditionObject(t).ExpectStatusConditionRead(
					unikornv1.ConditionHealthy,
					nil,
					testutil.ErrMustFail,
				)
			},
			Expected: openapi.ResourceHealthStatusUnknown,
		},
		successCase(
			"returns status healthy when condition reason is healthy",
			unikornv1.ConditionReasonHealthy,
			openapi.ResourceHealthStatusHealthy,
		),
		successCase(
			"returns status degraded when condition reason is degraded",
			unikornv1.ConditionReasonDegraded,
			openapi.ResourceHealthStatusDegraded,
		),
		successCase(
			"returns status error when condition reason is errored",
			unikornv1.ConditionReasonErrored,
			openapi.ResourceHealthStatusError,
		),
		successCase(
			"returns status unknown when condition reason is unrecognized",
			testutil.Gibberish,
			openapi.ResourceHealthStatusUnknown,
		),
	}

	for _, testCase := range testCases {
		t.Run(testCase.Name, func(t *testing.T) {
			t.Parallel()

			object := testCase.ObjectFunc(t)

			status := ConvertHealthCondition(object)
			require.Equal(t, testCase.Expected, status)
		})
	}
}
