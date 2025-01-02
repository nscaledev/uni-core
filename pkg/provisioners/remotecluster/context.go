/*
Copyright 2024-2025 the Unikorn Authors.

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

package remotecluster

import (
	"context"
)

type key int

const (
	// backgroundDeletionKey is used to propagate background deletion to
	// all descendant provisioners in the call graph.
	backgroundDeletionKey key = iota
)

func NewContextWithBackgroundDeletion(ctx context.Context, backgroundDeletion bool) context.Context {
	return context.WithValue(ctx, backgroundDeletionKey, backgroundDeletion)
}

func BackgroundDeletionFromContext(ctx context.Context) bool {
	if value := ctx.Value(backgroundDeletionKey); value != nil {
		if backgroundDeletion, ok := value.(bool); ok {
			return backgroundDeletion
		}
	}

	return false
}
