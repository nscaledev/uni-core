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

package retry

import (
	"context"
	"time"
)

// Error allows the last error to be retrieved.
type Error struct {
	context  error
	callback error
}

// Error implements the error interface.
func (e *Error) Error() string {
	return e.callback.Error()
}

// Unwrap allows access to either of the errors via errors.Is or errors.As.
func (e *Error) Unwrap() []error {
	return []error{
		e.context,
		e.callback,
	}
}

// Context gets the context error.
func (e *Error) Context() error {
	return e.context
}

// Callback gets the last callback error.
func (e *Error) Callback() error {
	return e.callback
}

// Callback is a callback that must return nil to escape the retry loop.
type Callback func() error

// Retrier implements retry loop logic.
type Retrier struct {
	// period defines the default retry period, defaulting to 1 second.
	period time.Duration
}

// Froever returns a retrier that will retry soething forever until a nil error
// is returned.
func Forever() *Retrier {
	return &Retrier{
		period: time.Second,
	}
}

// Do starts the retry loop.  It will run until success or until an optional
// timeout expires.
func (r *Retrier) Do(f Callback) error {
	return r.DoWithContext(context.TODO(), f)
}

// DoWithContext allows you to use a global context to interrupt execution.
func (r *Retrier) DoWithContext(c context.Context, f Callback) error {
	// Check immediately to avoid a delay of period.
	rerr := f()
	if rerr == nil {
		return nil
	}

	t := time.NewTicker(r.period)
	defer t.Stop()

	for {
		select {
		case <-c.Done():
			return &Error{
				context:  c.Err(),
				callback: rerr,
			}
		case <-t.C:
			if rerr = f(); rerr != nil {
				break
			}

			return nil
		}
	}
}
