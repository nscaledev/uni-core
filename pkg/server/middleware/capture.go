/*
Copyright 2025 the Unikorn Authors.

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

package middleware

import (
	"bytes"
	"io"
	"net/http"

	"github.com/felixge/httpsnoop"
)

type Capture struct {
	body *bytes.Buffer
	code int
}

// NewCapture creates a ready-to-use Capture. You use it by calling `Wrap` on an `http.ResponseWriter`,
// using the result in a handler, then examining the capture StatusCode() and Body().
// The convenience `CaptureResponse(...)` below does the above given the http.Handler, and hands you
// back the capture.
func NewCapture() *Capture {
	return &Capture{
		body: &bytes.Buffer{},
		code: http.StatusOK,
	}
}

func (c *Capture) StatusCode() int {
	return c.code
}

func (c *Capture) Body() *bytes.Buffer {
	return c.body
}

// Wrap creates a http.ResponseWriter which will capture the response in the method receiver so it
// can be examined after being used in a handler. Consider using `CaptureResponse()`, which does this
// for you.
func (c *Capture) Wrap(w http.ResponseWriter) http.ResponseWriter {
	return httpsnoop.Wrap(w, httpsnoop.Hooks{
		Write: func(next httpsnoop.WriteFunc) httpsnoop.WriteFunc {
			return func(p []byte) (int, error) {
				n, err := next(p)
				c.body.Write(p[:n]) // this always succeeds, for bytes.Buffer (unless it panics!)

				return n, err
			}
		},
		WriteHeader: func(next httpsnoop.WriteHeaderFunc) httpsnoop.WriteHeaderFunc {
			return func(statuscode int) {
				c.code = statuscode
				next(statuscode)
			}
		},
		ReadFrom: func(next httpsnoop.ReadFromFunc) httpsnoop.ReadFromFunc {
			return func(src io.Reader) (int64, error) {
				tee := io.TeeReader(src, c.body)
				return next(tee)
			}
		},
	})
}

// CaptureResponse runs the given http.Handler and returns the captured response.
// To see headers, look at `w.Header()`, which will be mutated by the handler.
func CaptureResponse(w http.ResponseWriter, r *http.Request, next http.Handler) *Capture {
	capture := NewCapture()
	next.ServeHTTP(capture.Wrap(w), r)

	return capture
}
