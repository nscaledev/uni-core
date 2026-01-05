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

package saga_test

import (
	"context"
	"errors"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/unikorn-cloud/core/pkg/server/saga"
)

var (
	errFailAction     = errors.New("fail action")
	errFailCompensate = errors.New("fail compensation")
)

type Handler struct {
	action1Result error
	action2Result error
	action3Result error

	compensate1Result error
	compensate2Result error

	action1Called bool
	action2Called bool
	action3Called bool

	compensate1Called bool
	compensate2Called bool
}

func (h *Handler) action1(ctx context.Context) error {
	h.action1Called = true
	return h.action1Result
}

func (h *Handler) action2(ctx context.Context) error {
	h.action2Called = true
	return h.action2Result
}

func (h *Handler) action3(ctx context.Context) error {
	h.action3Called = true
	return h.action3Result
}

func (h *Handler) compensate1(ctx context.Context) error {
	h.compensate1Called = true
	return h.compensate1Result
}

func (h *Handler) compensate2(ctx context.Context) error {
	h.compensate2Called = true
	return h.compensate2Result
}

func (h *Handler) Actions() []saga.Action {
	return []saga.Action{
		saga.NewAction("action1", h.action1, h.compensate1),
		saga.NewAction("action2", h.action2, h.compensate2),
		saga.NewAction("action3", h.action3, nil),
	}
}

// TestSaga ensures all actions are called.
func TestSaga(t *testing.T) {
	t.Parallel()

	h := &Handler{}

	require.NoError(t, saga.Run(t.Context(), h))
	require.True(t, h.action1Called)
	require.True(t, h.action2Called)
	require.True(t, h.action3Called)
	require.False(t, h.compensate1Called)
	require.False(t, h.compensate2Called)
}

// TestSagaFailAction1 ensures compensating actions are correctly run.
func TestSagaFailAction1(t *testing.T) {
	t.Parallel()

	h := &Handler{
		action1Result: errFailAction,
	}

	require.ErrorIs(t, saga.Run(t.Context(), h), errFailAction)
	require.True(t, h.action1Called)
	require.False(t, h.action2Called)
	require.False(t, h.action3Called)
	require.False(t, h.compensate1Called)
	require.False(t, h.compensate2Called)
}

// TestSagaFailAction3 ensures compensating actions are correctly run.
func TestSagaFailAction3(t *testing.T) {
	t.Parallel()

	h := &Handler{
		action3Result: errFailAction,
	}

	require.ErrorIs(t, saga.Run(t.Context(), h), errFailAction)
	require.True(t, h.action1Called)
	require.True(t, h.action2Called)
	require.True(t, h.action3Called)
	require.True(t, h.compensate1Called)
	require.True(t, h.compensate2Called)
}

// TestSagaFailCompensation2 tests that compensation errors short circuit
// and that the error returned is that of the failing action.
func TestSagaFailCompensation2(t *testing.T) {
	t.Parallel()

	h := &Handler{
		action3Result:     errFailAction,
		compensate2Result: errFailCompensate,
	}

	require.ErrorIs(t, saga.Run(t.Context(), h), errFailAction)
	require.True(t, h.action1Called)
	require.True(t, h.action2Called)
	require.True(t, h.action3Called)
	require.False(t, h.compensate1Called)
	require.True(t, h.compensate2Called)
}
