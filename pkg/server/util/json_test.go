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

package util_test

import (
	"bytes"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/require"

	servererrors "github.com/unikorn-cloud/core/pkg/server/errors"
	"github.com/unikorn-cloud/core/pkg/server/util"
)

var errSimulatedRead = errors.New("simulated read error")

type testPayload struct {
	Name string `json:"name"`
}

type errReader struct{}

func (errReader) Read([]byte) (int, error) { return 0, errSimulatedRead }

func TestReadJSONBody(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name      string
		bodyBytes []byte
		useErr    bool
		expectErr bool
		isBadReq  bool
	}{
		{
			name:      "EmptyBody",
			bodyBytes: nil,
			expectErr: true,
			isBadReq:  true,
		},
		{
			name:      "MalformedJSON",
			bodyBytes: []byte("{invalid"),
			expectErr: true,
			isBadReq:  true,
		},
		{
			name:      "ReadError",
			useErr:    true,
			expectErr: true,
			isBadReq:  false,
		},
		{
			name:      "ValidBody",
			bodyBytes: []byte(`{"name":"test"}`),
			expectErr: false,
		},
	}

	for i := range tests {
		tc := &tests[i]

		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			var r *http.Request
			if tc.useErr {
				r = httptest.NewRequestWithContext(t.Context(), http.MethodPost, "/", errReader{})
			} else {
				r = httptest.NewRequestWithContext(t.Context(), http.MethodPost, "/", bytes.NewReader(tc.bodyBytes))
			}

			var p testPayload

			err := util.ReadJSONBody(r, &p)

			if tc.expectErr {
				require.Error(t, err)
				require.Equal(t, tc.isBadReq, servererrors.IsBadRequest(err))
			} else {
				require.NoError(t, err)
				require.Equal(t, "test", p.Name)
			}
		})
	}
}
