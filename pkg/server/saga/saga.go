/*
Copyright 2025 the Unikorn Authors.
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

package saga

import (
	"context"

	"sigs.k8s.io/controller-runtime/pkg/log"
)

// ActionFunc is a generic action/compensation function.
// They will typically be bound receivers so that saga steps can
// share state between themselves.
type ActionFunc func(ctx context.Context) error

// Action is a single step in a saga.
type Action struct {
	// name is used for logging so we can see what went wrong.
	name string
	// action is what is executed on the good path.
	action ActionFunc
	// compensate is run if a subsequent action in the saga fails
	// and can undo any state changes that need to be rewound.
	// May be nil.
	compensate ActionFunc
}

// NewAction creates a new action.
func NewAction(name string, action, compensate ActionFunc) Action {
	return Action{
		name:       name,
		action:     action,
		compensate: compensate,
	}
}

// Handler implements a saga, a set of steps to achieve a desired outcome
// and a set of steps to undo any state changes on failure of an action.
type Handler interface {
	Actions() []Action
}

// Run implements the saga algorithm.
func Run(ctx context.Context, handler Handler) error {
	log := log.FromContext(ctx)

	actions := handler.Actions()

	// Do each action in order...
	for i := range actions {
		if err := actions[i].action(ctx); err != nil {
			// If something went wrong we need to undo all prior steps
			// to compensate for any changed state e.g. quota allocations.
			for j := i - 1; j >= 0; j-- {
				if actions[j].compensate == nil {
					continue
				}

				if cerr := actions[j].compensate(ctx); cerr != nil {
					// You see this in your logs, you're going to have to
					// do some manual unpicking!
					// TODO: we could add a retry in here for transient errors
					// (and the actual action itself), but we aware the client
					// and server will have a response timeout, so perhaps
					// adding the compensation action to a log for aysnchronous
					// handling may be better in future.
					log.Error(cerr, "compensating action failed", "name", actions[j].name)
					return err
				}
			}

			// Always return the error that caused failure, which will most likely
			// be something useful to user like quota allocation failures.
			return err
		}
	}

	return nil
}
