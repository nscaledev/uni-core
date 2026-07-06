/*
Copyright 2022-2024 EscherCloud.
Copyright 2024-2025 the Unikorn Authors.
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

package provisioners

import (
	"errors"
	"fmt"

	unikornv1 "github.com/unikorn-cloud/core/pkg/apis/unikorn/v1alpha1"
	"github.com/unikorn-cloud/core/pkg/constants"

	"k8s.io/apimachinery/pkg/runtime"

	"sigs.k8s.io/controller-runtime/pkg/client"
)

var (
	// ErrYield is raised when a provision/deprovision optation could
	// block for a long time, in particular the bits that wait for apllication
	// available status.  This will trigger a controller to requeue the request.
	// The key things are that workers are unblocked, allowing other reconciles
	// to be triggered, and we can pick up an modifications (e.g. the cluster is
	// gubbed - thanks CAPO - and we can delete it without waiting for 10m as the
	// case used to be in the old world.
	ErrYield = errors.New("controller timeout yield")

	// ErrNotFound is when a resource is not found.
	ErrNotFound = errors.New("resource not found")

	// ErrTerminal marks a provisioning failure that will not self-heal and must
	// stop being requeued.
	//
	// WHY: today a provisioner that returns any error other than ErrYield is
	// requeued at a fixed interval forever (see reconcileNormal). There is no
	// way to say "give up". That is correct for transient faults but wrong for
	// permanent ones: a server whose provider create has been retried to
	// exhaustion, or a request that can never succeed, will otherwise spin the
	// workqueue indefinitely (observed in the wild: a counter climbing into the
	// hundreds against a cap of 3). Revival of an ErrTerminal resource is an
	// operator concern only — nothing the owning user can do to their own spec
	// will unstick it; see ErrUserActionRequired for the recoverable variant.
	//
	// This sentinel is intentionally a disposition, not a message. It is meant
	// to be matched with errors.Is so the manager can branch on it (stop
	// requeuing, write a terminal condition) rather than parsing prose.
	ErrTerminal = errors.New("provisioning terminally failed")

	// ErrUserActionRequired marks a provisioning failure that is terminal *for
	// now* but that the owning user can clear by changing the resource spec.
	//
	// WHY: a deterministic, user-caused failure (an invalid flavour, image, or
	// configuration) should not retry forever — retrying identical bad input is
	// pointless and hides the real problem — but it is also not a support case.
	// The remedy is in the user's hands: edit the spec. Controllers already wake
	// on a generation bump, so an ErrUserActionRequired resource naturally
	// resumes when (and only when) the spec changes. Splitting this from
	// ErrTerminal keeps "you can fix this" distinct from "call an operator",
	// which are different things to tell a user and different recovery paths.
	//
	// NOTE: making this disposition actually recoverable requires the owning
	// controller to clear any retry bookkeeping (e.g. attempt counters) on a
	// generation change; the sentinel alone does not do that.
	ErrUserActionRequired = errors.New("provisioning requires user action")
)

// Error is a provisioning error that pairs a disposition with a user-safe,
// machine-classifiable surface.
//
// The manager funnels every provisioner error through a single condition
// message. Stringifying a raw error there is a fail-open leak of internal detail
// (CWE-209); at the same time, a bare code is unhelpful to the human reading it.
// Error separates the pieces that serve the different audiences:
//
//   - disposition: the machine-switchable outcome (ErrTerminal,
//     ErrUserActionRequired, ErrYield), exposed via Unwrap so errors.Is keeps
//     working. Lives in the type system, never in prose.
//   - reason: a closed-vocabulary ProvisioningConditionReason, machine-classifiable
//     and written straight onto the Available condition's Reason field, e.g.
//     DependencyFailed.
//   - message: user-safe human detail, written onto the condition's Message
//     field, e.g. Network "prod-net" (a1b2) is not ready. It IS shown to the
//     user, so it must be safe — name the user's own resources, never internal
//     topology.
//
// The manager surfaces reason and message directly onto the condition via
// SetProvisioningCondition (see Reason/Message); there is no flattening into a
// single string. Genuinely operator-only detail (provider internals, raw
// upstream errors) does NOT go in message: wrap the Error with fmt.Errorf
// instead. Error() embeds the wrapping for logs, and the manager's errors.As
// recovers the typed reason/message so the unsafe wrapping never reaches the
// user surface.
type Error struct {
	// disposition is the sentinel this error unwraps to (terminal, yield, ...).
	disposition error
	// reason is the closed-vocabulary provisioning condition reason.
	reason unikornv1.ProvisioningConditionReason
	// message is user-safe human detail, surfaced as the condition message.
	message string
}

func newError(disposition error, reason unikornv1.ProvisioningConditionReason, message string) *Error {
	return &Error{disposition: disposition, reason: reason, message: message}
}

// Error implements the error interface, embedding the reason and message so
// existing, unmodified logging surfaces them for free.
func (e *Error) Error() string {
	if e.message == "" {
		return fmt.Sprintf("%s: %s", e.disposition, e.reason)
	}

	return fmt.Sprintf("%s: %s: %s", e.disposition, e.reason, e.message)
}

// Unwrap exposes the disposition sentinel so callers can branch with
// errors.Is(err, ErrTerminal) etc.
func (e *Error) Unwrap() error {
	return e.disposition
}

// Reason returns the closed-vocabulary provisioning reason to write onto the
// Available condition's Reason field.
func (e *Error) Reason() unikornv1.ProvisioningConditionReason {
	return e.reason
}

// Message returns the user-safe human detail to write onto the condition's
// Message field.
func (e *Error) Message() string {
	return e.message
}

// Terminal returns an ErrTerminal-dispositioned error: permanent, operator-only
// recovery. See ErrTerminal.
func Terminal(reason unikornv1.ProvisioningConditionReason, message string) *Error {
	return newError(ErrTerminal, reason, message)
}

// UserActionRequired returns an ErrUserActionRequired-dispositioned error:
// terminal until the user changes the spec. See ErrUserActionRequired.
func UserActionRequired(reason unikornv1.ProvisioningConditionReason, message string) *Error {
	return newError(ErrUserActionRequired, reason, message)
}

// Yield returns an ErrYield-dispositioned error carrying a reason code and
// user-safe message while still requeuing. It is the ErrYield sibling of Terminal
// and UserActionRequired.
func Yield(reason unikornv1.ProvisioningConditionReason, message string) *Error {
	return newError(ErrYield, reason, message)
}

// describeResource renders a stable identifier for a resource for use in a
// dependency Error's message: the Kind (from the scheme's GVK), the display name
// (a point-in-time label value, mutable), and the durable id (the object name).
// The id is the anchor; the name is a convenience that may have changed since.
//
//	Network "my-prod-net" (a1b2c3d4-...)
//
// If the name label is absent — the object was never fetched, or has been
// deleted — only the Kind and id are rendered. All of this is the user's own
// resource, hence safe to surface.
func describeResource(scheme *runtime.Scheme, o client.Object) string {
	kind := "resource"

	if gvks, _, err := scheme.ObjectKinds(o); err == nil && len(gvks) > 0 {
		kind = gvks[0].Kind
	}

	id := o.GetName()

	name := o.GetLabels()[constants.NameLabel]
	if name == "" {
		return fmt.Sprintf("%s %s", kind, id)
	}

	return fmt.Sprintf("%s %q (%s)", kind, name, id)
}

// DependencyNotReady is the sanctioned way to signal a wait on a dependency that
// exists but is not yet provisioned. It binds the reason, the yield disposition,
// and a safe description together so a caller cannot mis-pair them.
func DependencyNotReady(scheme *runtime.Scheme, o client.Object) *Error {
	return Yield(unikornv1.ConditionReasonDependencyNotReady, describeResource(scheme, o)+" is not ready")
}

// DependencyFailed signals a wait on a dependency that is itself in an error
// state. It still yields (the dependency may recover), but names the failure so
// the wait is not mistaken for progress.
func DependencyFailed(scheme *runtime.Scheme, o client.Object) *Error {
	return Yield(unikornv1.ConditionReasonDependencyFailed, describeResource(scheme, o)+" has failed")
}

// DependencyNotFound signals a referenced dependency that does not exist. It is
// terminal: waiting cannot resolve it. A referenced, finalized dependency that is
// nonetheless gone is a consistency violation, not a transient wait.
func DependencyNotFound(scheme *runtime.Scheme, o client.Object) *Error {
	return Terminal(unikornv1.ConditionReasonDependencyNotFound, describeResource(scheme, o)+" does not exist")
}

// IsTerminal reports whether an error is a terminal provisioning disposition,
// i.e. one that must NOT be requeued.
//
// WHY: the manager's requeue decision needs a single, authoritative test for
// "stop retrying this" rather than open-coding the set of terminal sentinels at
// each call site (and drifting when a new one is added). Both ErrTerminal and
// ErrUserActionRequired are terminal in the requeue sense — neither will be
// helped by another immediate reconcile. They differ only in how they are
// revived (operator vs spec change), which is a concern for the watch/predicate
// layer, not for the requeue decision.
func IsTerminal(err error) bool {
	return errors.Is(err, ErrTerminal) || errors.Is(err, ErrUserActionRequired)
}
