/*
Copyright The Kubernetes Authors.

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

package controller

import (
	"testing"

	. "github.com/onsi/gomega"
	"k8s.io/apimachinery/pkg/types"
)

func TestBootstrapAnnotationKey(t *testing.T) {
	g := NewWithT(t)

	uid := types.UID("550e8400-e29b-41d4-a716-446655440000")
	key := bootstrapAnnotationKey(uid)
	g.Expect(key).To(Equal("readiness.k8s.io/bootstrap-completed-550e8400-e29b-41d4-a716-446655440000"))
}

func TestBootstrapAnnotationValue(t *testing.T) {
	g := NewWithT(t)

	t.Run("encodes rule name as JSON", func(t *testing.T) {
		val := bootstrapAnnotationValue("my-rule")
		g.Expect(val).To(Equal(`{"rule":"my-rule"}`))
	})

	t.Run("handles long rule names", func(t *testing.T) {
		longName := "my-very-long-rule-name-that-exceeds-the-63-character-annotation-key-limit-strictly"
		val := bootstrapAnnotationValue(longName)
		g.Expect(val).To(ContainSubstring(longName))
	})
}

func TestLegacyBootstrapAnnotationKey(t *testing.T) {
	g := NewWithT(t)

	key := legacyBootstrapAnnotationKey("my-rule")
	g.Expect(key).To(Equal("readiness.k8s.io/bootstrap-completed-my-rule"))
}

func TestIsLegacyBootstrapKey(t *testing.T) {
	g := NewWithT(t)

	tests := []struct {
		name     string
		key      string
		expected bool
	}{
		{
			name:     "legacy key with rule name",
			key:      "readiness.k8s.io/bootstrap-completed-my-rule",
			expected: true,
		},
		{
			name:     "legacy key with long rule name",
			key:      "readiness.k8s.io/bootstrap-completed-my-very-long-rule-name-that-exceeds-limits",
			expected: true,
		},
		{
			name:     "UID-based key is NOT legacy",
			key:      "readiness.k8s.io/bootstrap-completed-550e8400-e29b-41d4-a716-446655440000",
			expected: false,
		},
		{
			name:     "unrelated annotation key",
			key:      "some-other-annotation",
			expected: false,
		},
		{
			name:     "prefix only with no suffix",
			key:      "readiness.k8s.io/bootstrap-completed-",
			expected: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			g.Expect(isLegacyBootstrapKey(tt.key)).To(Equal(tt.expected), "key: %s", tt.key)
		})
	}
}

func TestIsUUID(t *testing.T) {
	g := NewWithT(t)

	tests := []struct {
		name     string
		input    string
		expected bool
	}{
		{
			name:     "valid UUID",
			input:    "550e8400-e29b-41d4-a716-446655440000",
			expected: true,
		},
		{
			name:     "valid UUID with uppercase",
			input:    "550E8400-E29B-41D4-A716-446655440000",
			expected: true,
		},
		{
			name:     "too short",
			input:    "550e8400-e29b-41d4",
			expected: false,
		},
		{
			name:     "rule name (not UUID)",
			input:    "my-rule",
			expected: false,
		},
		{
			name:     "empty string",
			input:    "",
			expected: false,
		},
		{
			name:     "36 chars but wrong format",
			input:    "550e8400xe29bx41d4xa716x446655440000",
			expected: false,
		},
		{
			name:     "36 chars with invalid hex char",
			input:    "550g8400-e29b-41d4-a716-446655440000",
			expected: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			g.Expect(isUUID(tt.input)).To(Equal(tt.expected), "input: %s", tt.input)
		})
	}
}
