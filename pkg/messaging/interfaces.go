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

package messaging

import (
	"context"
)

type Consumer interface {
	// Consume consumes an event from the queue.
	Consume(ctx context.Context, envelope *Envelope) error
}

// Queue is an abstract message queue client, the exact implementation
// is defined by the implementation.  A queue must always replay all active
// resources, so we can witness missed events on a restart.  If an error is
// encontered when the consumer is invoked, then the event must be requeued
// to mitigate transient errors.
type Queue interface {
	// Run starts the event queue consumption.  This is a blocking call.
	Run(ctx context.Context, consumers ...Consumer) error
}
