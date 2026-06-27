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

// Error is a provisioning error that carries an explicit, structured why
// alongside its disposition.
//
// WHY: the manager funnels every provisioner error through a single condition
// message, and the default path stringifies the raw error into user-visible
// text — a fail-open leak of internal detail (CWE-209). At the same time, an
// operator reading logs wants exactly that detail. One free-text field cannot
// honestly serve both audiences. This type separates the three things that
// actually matter:
//
//   - disposition: the machine-switchable outcome (ErrTerminal,
//     ErrUserActionRequired, ErrYield, ...), exposed via Unwrap so errors.Is
//     keeps working. Disposition lives in the type system, never in prose.
//   - reason: a short, stable code (e.g. "insufficient_capacity",
//     "invalid_flavor") suitable for mapping to a closed user-facing
//     vocabulary later, without re-parsing strings.
//   - why: an operator-facing detail string for logs and debugging. It is NOT
//     intended to reach the user verbatim; routing it to a user surface is a
//     deliberate, separate decision.
//
// The quick win it buys today, with no wiring: Error() embeds the why, so the
// existing log.Error(err, ...) call in the manager logs the reason and why for
// free — you get useful operator logs the moment a provisioner returns one of
// these, well before any condition-surfacing work lands.
type Error struct {
	// disposition is the sentinel this error unwraps to (terminal, yield, ...).
	disposition error
	// reason is a short stable code identifying the failure class.
	reason string
	// why is operator-facing detail; safe for logs, not for users.
	why string
}

func newError(disposition error, reason, why string) *Error {
	return &Error{disposition: disposition, reason: reason, why: why}
}

// Error implements the error interface. The why is included so existing,
// unmodified logging surfaces it without any extra plumbing.
func (e *Error) Error() string {
	if e.why == "" {
		return fmt.Sprintf("%s: %s", e.disposition, e.reason)
	}

	return fmt.Sprintf("%s: %s: %s", e.disposition, e.reason, e.why)
}

// Unwrap exposes the disposition sentinel so callers can branch with
// errors.Is(err, ErrTerminal) etc.
func (e *Error) Unwrap() error {
	return e.disposition
}

// Reason returns the short stable failure code.
func (e *Error) Reason() string {
	return e.reason
}

// Why returns the operator-facing detail. Treat as log-safe, not user-safe.
func (e *Error) Why() string {
	return e.why
}

// Terminal returns an ErrTerminal-dispositioned error: permanent, operator-only
// recovery. See ErrTerminal.
func Terminal(reason, why string) *Error {
	return newError(ErrTerminal, reason, why)
}

// UserActionRequired returns an ErrUserActionRequired-dispositioned error:
// terminal until the user changes the spec. See ErrUserActionRequired.
func UserActionRequired(reason, why string) *Error {
	return newError(ErrUserActionRequired, reason, why)
}

// Blocked returns a yield-dispositioned error that names the dependency being
// waited on.
//
// WHY: dependency waits currently collapse into a single opaque "Provisioning"
// state — you cannot tell a resource that is doing work from one parked on a
// dependency. Blocked still requeues (it unwraps to ErrYield), but records which
// resource is the blocker. The kind/id is the user's own resource, so it is safe
// to surface, and it makes the wait self-explanatory in logs immediately.
func Blocked(kind, id string) *Error {
	return newError(ErrYield, "dependency_not_ready", fmt.Sprintf("%s/%s", kind, id))
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
