/*
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

package client_test

import (
	"os"
	"sync"
	"testing"

	"github.com/go-logr/logr"

	"sigs.k8s.io/controller-runtime/pkg/log"
)

// records every log line emitted by the package under test.  Installed once for
// the whole binary so parallel tests never swap the global logger underneath one
// another; tests disambiguate by the secret name in the key/value pairs.
type logRecorder struct {
	mu    sync.Mutex
	lines []logLine
}

type logLine struct {
	message string
	keys    map[string]any
}

//nolint:gochecknoglobals // the logger it records is process wide, so this must be too.
var recorder = &logRecorder{}

func (r *logRecorder) record(message string, kv ...any) {
	r.mu.Lock()
	defer r.mu.Unlock()

	keys := map[string]any{}

	for i := 0; i+1 < len(kv); i += 2 {
		if name, ok := kv[i].(string); ok {
			keys[name] = kv[i+1]
		}
	}

	r.lines = append(r.lines, logLine{message: message, keys: keys})
}

// forget drops everything recorded against one secret.  The recorder lives for the
// life of the test binary, so a test that asserts on counts MUST clear its own key
// first or it fails on the second run under -count.
func (r *logRecorder) forget(secret string) {
	r.mu.Lock()
	defer r.mu.Unlock()

	kept := r.lines[:0]

	for _, line := range r.lines {
		if line.keys["secret"] != secret {
			kept = append(kept, line)
		}
	}

	r.lines = kept
}

// messagesFor returns the messages logged against one secret, in order.
func (r *logRecorder) messagesFor(secret string) []string {
	r.mu.Lock()
	defer r.mu.Unlock()

	var out []string

	for _, line := range r.lines {
		if line.keys["secret"] == secret {
			out = append(out, line.message)
		}
	}

	return out
}

type recordingSink struct {
	recorder *logRecorder
	values   []any
}

func (s *recordingSink) Init(logr.RuntimeInfo)        {}
func (s *recordingSink) Enabled(int) bool             { return true }
func (s *recordingSink) WithName(string) logr.LogSink { return s }

func (s *recordingSink) WithValues(kv ...any) logr.LogSink {
	return &recordingSink{recorder: s.recorder, values: append(append([]any{}, s.values...), kv...)}
}

func (s *recordingSink) Info(_ int, message string, kv ...any) {
	s.recorder.record(message, append(append([]any{}, s.values...), kv...)...)
}

func (s *recordingSink) Error(_ error, message string, kv ...any) {
	s.recorder.record(message, append(append([]any{}, s.values...), kv...)...)
}

func TestMain(m *testing.M) {
	log.SetLogger(logr.New(&recordingSink{recorder: recorder}))

	os.Exit(m.Run())
}
