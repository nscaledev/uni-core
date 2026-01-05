/*
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

package manager

import (
	"context"

	"sigs.k8s.io/controller-runtime/pkg/manager"
)

type contextkeyType int

//nolint:gochecknoglobals
var contextkey contextkeyType

func NewContext(ctx context.Context, manager manager.Manager) context.Context {
	return context.WithValue(ctx, contextkey, manager)
}

func FromContext(ctx context.Context) manager.Manager {
	//nolint:forcetypeassert
	return ctx.Value(contextkey).(manager.Manager)
}
