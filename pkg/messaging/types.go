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
	"time"
)

// Envelope is a generic messaging envelope for resource messages.
type Envelope struct {
	// ResourceID the GUID of a resource.
	ResourceID string
	// DeletionTimestamp describes whether the resource is being deleted
	// or not, and is used for routing.  If not set this is a creation or
	// update event.
	DeletionTimestamp *time.Time
}
