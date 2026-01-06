/*
Copyright 2022-2023 EscherCloud.
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

package v1alpha1_test

import (
	"net"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/unikorn-cloud/core/pkg/apis/unikorn/v1alpha1"
)

const (
	// Expect IP addresses to be marshalled as strings in dotted quad format.
	testAddressMarshaled    = `"192.168.0.1"`
	testAddressUnstructured = "192.168.0.1"

	// Expect IP prefixes to be marshalled as strings in dotted quad CIDR format.
	testPrefixMarshaled    = `"192.168.0.0/16"`
	testPrefixUnstructured = "192.168.0.0/16"
)

var (
	// Expect IP addresses to be unmarshalled as an IPv4 address.
	//nolint:gochecknoglobals
	testAddressUnmarshaled = net.IPv4(192, 168, 0, 1)

	// Expect IP prefixes to be unmarshalled as an IPv4 network.
	//nolint:gochecknoglobals
	testPrefixUnmarshaled = net.IPNet{
		IP:   net.IPv4(192, 168, 0, 0),
		Mask: net.IPv4Mask(255, 255, 0, 0),
	}
)

func TestSemanticVersionCanonical(t *testing.T) {
	t.Parallel()

	jsonSemver := `"1.2.3-foo+bar"`
	unstructuredSemver := "1.2.3-foo+bar"

	out := &v1alpha1.SemanticVersion{}

	require.NoError(t, out.UnmarshalJSON([]byte(jsonSemver)))
	require.EqualValues(t, 1, out.Major())
	require.EqualValues(t, 2, out.Minor())
	require.EqualValues(t, 3, out.Patch())

	marshalled, err := out.MarshalJSON()
	require.NoError(t, err)
	require.Equal(t, jsonSemver, string(marshalled))

	unstructured := out.ToUnstructured()
	require.Equal(t, unstructuredSemver, unstructured)
}

func TestSemanticVersion(t *testing.T) {
	t.Parallel()

	jsonSemver := `"v1.2.3-foo+bar"`

	out := &v1alpha1.SemanticVersion{}

	require.NoError(t, out.UnmarshalJSON([]byte(jsonSemver)))
	require.EqualValues(t, 1, out.Major())
	require.EqualValues(t, 2, out.Minor())
	require.EqualValues(t, 3, out.Patch())

	marshalled, err := out.MarshalJSON()
	require.NoError(t, err)
	require.Equal(t, jsonSemver, string(marshalled))
}

func TestConstraints(t *testing.T) {
	t.Parallel()

	good := &v1alpha1.SemanticVersion{}
	require.NoError(t, good.UnmarshalJSON([]byte(`"1.5.0"`)))

	bad := &v1alpha1.SemanticVersion{}
	require.NoError(t, bad.UnmarshalJSON([]byte(`"3.0.0"`)))

	jsonConstraints := `">= 1.0.0, < 2.0.0"`

	out := &v1alpha1.SemanticVersionConstraints{}

	require.NoError(t, out.UnmarshalJSON([]byte(jsonConstraints)))
	require.True(t, out.Check(good))
	require.False(t, out.Check(bad))

	// NOTE: This emits UTF8, which isn't the same as the input.
	// We could do some text transformation I guess...
	_, err := out.MarshalJSON()
	require.NoError(t, err)
}

func TestIPv4AddressUnmarshal(t *testing.T) {
	t.Parallel()

	input := []byte(testAddressMarshaled)

	output := &v1alpha1.IPv4Address{}

	require.NoError(t, output.UnmarshalJSON(input))
	require.Equal(t, testAddressUnmarshaled, output.IP)
}

func TestIPv4AddressMarshal(t *testing.T) {
	t.Parallel()

	input := &v1alpha1.IPv4Address{IP: testAddressUnmarshaled}

	output, err := input.MarshalJSON()
	require.NoError(t, err)

	require.Equal(t, testAddressMarshaled, string(output))

	unstructured := input.ToUnstructured()
	require.Equal(t, testAddressUnstructured, unstructured)
}

func TestIPv4PrefixUnmarshal(t *testing.T) {
	t.Parallel()

	input := []byte(testPrefixMarshaled)

	output := &v1alpha1.IPv4Prefix{}

	require.NoError(t, output.UnmarshalJSON(input))
	require.Equal(t, testPrefixUnmarshaled.String(), output.String())
}

func TestIPv4PrefixMarshal(t *testing.T) {
	t.Parallel()

	input := &v1alpha1.IPv4Prefix{IPNet: testPrefixUnmarshaled}

	output, err := input.MarshalJSON()
	require.NoError(t, err)
	require.Equal(t, testPrefixMarshaled, string(output))

	unstructured := input.ToUnstructured()
	require.Equal(t, testPrefixUnstructured, unstructured)
}
