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

package v1alpha1

import (
	"errors"
	"fmt"
	"iter"
	"strings"
)

var (
	// ErrVersionNotFound is raised when the requested version is
	// undefined in an application.
	ErrVersionNotFound = errors.New("version not found")
)

func CompareHelmApplication(a, b HelmApplication) int {
	return strings.Compare(a.Name, b.Name)
}

// Versions returns an iterator over versions.
func (a *HelmApplication) Versions() iter.Seq[*HelmApplicationVersion] {
	return func(yield func(*HelmApplicationVersion) bool) {
		for i := range a.Spec.Versions {
			if !yield(&a.Spec.Versions[i]) {
				break
			}
		}
	}
}

func (a *HelmApplication) GetVersion(version SemanticVersion) (*HelmApplicationVersion, error) {
	for v := range a.Versions() {
		if v.Version.Equal(&version) {
			return v, nil
		}
	}

	return nil, fmt.Errorf("%w: %v", ErrVersionNotFound, version.Version)
}
