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

package manager

import (
	"context"
	"errors"
	"net"

	kerrors "k8s.io/apimachinery/pkg/api/errors"
)

// WithTransient makes a provision that failed because it could not reach a
// service leave the Available condition as it was, while still saving the rest
// of status.  Core classifies network errors, timeouts (a context deadline
// included) and the Kubernetes errors that mean "could not reach";
// classifiers add the errors of the external systems the consumer owns.  A
// failure with a disposition is never transient, and the delete path ignores
// the classifier.  See
// the README's "Invariants And Guard Rails" section.
func WithTransient(classifiers ...func(error) bool) ReconcilerOption {
	return func(r *Reconciler) {
		r.transient = func(err error) bool {
			return Unreachable(err, classifiers...)
		}
	}
}

// Unreachable reports whether a failure says a service could not be reached:
// the test WithTransient applies, exported so a provisioner that keeps its own
// status in step with the condition agrees with it.  A failure with a
// disposition is not, because the provisioner decided what to report, and nor
// is a cancelled call, which is shutdown though the client wraps it in a
// network error.
func Unreachable(err error, classifiers ...func(error) bool) bool {
	if err == nil || handled(err) || errors.Is(err, context.Canceled) {
		return false
	}

	if transient(err) {
		return true
	}

	for _, classifier := range classifiers {
		if classifier(err) {
			return true
		}
	}

	return false
}

// unreachable reports whether WithTransient is set and err is Unreachable.
func (r *Reconciler) unreachable(err error) bool {
	return r.transient != nil && r.transient(err)
}

// transient reports whether err says a service could not be reached, rather
// than giving an answer.  An internal error or a conflict is an answer.
// context.DeadlineExceeded is a net.Error, so a timeout is transient.
func transient(err error) bool {
	var netErr net.Error
	if errors.As(err, &netErr) {
		return true
	}

	return kerrors.IsServerTimeout(err) ||
		kerrors.IsTimeout(err) ||
		kerrors.IsTooManyRequests(err) ||
		kerrors.IsServiceUnavailable(err)
}
