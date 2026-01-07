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

//nolint:testpackage
package conversion

import (
	"math/rand"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
	"go.uber.org/mock/gomock"

	unikornv1 "github.com/unikorn-cloud/core/pkg/apis/unikorn/v1alpha1"
	mockv1 "github.com/unikorn-cloud/core/pkg/apis/unikorn/v1alpha1/mock"
	"github.com/unikorn-cloud/core/pkg/constants"
	"github.com/unikorn-cloud/core/pkg/openapi"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/utils/ptr"
)

type FakeConditionObject struct {
	metav1.ObjectMeta
	*mockv1.MockStatusConditionReader
}

func NewFakeConditionObject(t *testing.T, metadata metav1.ObjectMeta) *FakeConditionObject {
	t.Helper()

	mockController := gomock.NewController(t)
	t.Cleanup(mockController.Finish)

	mockReader := mockv1.NewMockStatusConditionReader(mockController)

	return &FakeConditionObject{
		ObjectMeta:                metadata,
		MockStatusConditionReader: mockReader,
	}
}

func (o *FakeConditionObject) ExpectStatusConditionRead(conditionType unikornv1.ConditionType, reason unikornv1.ConditionReason) *FakeConditionObject {
	condition := &unikornv1.Condition{
		Reason: reason,
	}

	o.MockStatusConditionReader.
		EXPECT().
		StatusConditionRead(conditionType).
		Return(condition, nil)

	return o
}

func TestResourceReadMetadata(t *testing.T) {
	t.Parallel()

	type TestCase struct {
		Name       string
		ObjectFunc func(t *testing.T) metav1.Object
		Tags       unikornv1.TagList
		Expected   openapi.ResourceReadMetadata
	}

	testCases := []TestCase{
		{
			Name: "#1",
			ObjectFunc: func(t *testing.T) metav1.Object {
				t.Helper()

				metadata := metav1.ObjectMeta{
					Name:              "23cd931f-f34a-496c-8aa8-5157bbefe986",
					CreationTimestamp: metav1.Date(2026, 1, 1, 9, 0, 0, 0, time.UTC),
					DeletionTimestamp: ptr.To(metav1.Date(2026, 1, 1, 9, 30, 0, 0, time.UTC)),
					Labels: map[string]string{
						constants.NameLabel: "16f22cd2-81dc-4d8c-b8b8-71e6ef709a0b",
					},
					Annotations: map[string]string{
						constants.DescriptionAnnotation:       "17809d4c-80ef-4f77-bb1d-dc3fbad13f49",
						constants.CreatorAnnotation:           "ff369398-9761-4c78-b776-be5e9d6cb3cc",
						constants.ModifierAnnotation:          "2174f086-cb08-4b1c-b652-e08e93b0acd6",
						constants.ModifiedTimestampAnnotation: "2026-01-01T09:15:00Z",
					},
				}

				return NewFakeConditionObject(t, metadata).
					ExpectStatusConditionRead(unikornv1.ConditionHealthy, unikornv1.ConditionReasonDegraded)
			},
			Tags: unikornv1.TagList{
				{
					Name:  "922cee79-843a-4648-813a-56933695d49e",
					Value: "d963d489-ddd5-4119-9256-7b3de2ca4fd9",
				},
			},
			Expected: openapi.ResourceReadMetadata{
				CreatedBy:          ptr.To("ff369398-9761-4c78-b776-be5e9d6cb3cc"),
				CreationTime:       time.Date(2026, 1, 1, 9, 0, 0, 0, time.UTC),
				DeletionTime:       ptr.To(time.Date(2026, 1, 1, 9, 30, 0, 0, time.UTC)),
				Description:        ptr.To("17809d4c-80ef-4f77-bb1d-dc3fbad13f49"),
				HealthStatus:       openapi.ResourceHealthStatusDegraded,
				Id:                 "23cd931f-f34a-496c-8aa8-5157bbefe986",
				ModifiedBy:         ptr.To("2174f086-cb08-4b1c-b652-e08e93b0acd6"),
				ModifiedTime:       ptr.To(time.Date(2026, 1, 1, 9, 15, 0, 0, time.UTC)),
				Name:               "16f22cd2-81dc-4d8c-b8b8-71e6ef709a0b",
				ProvisioningStatus: openapi.ResourceProvisioningStatusDeprovisioning,
				Tags: &openapi.TagList{
					{
						Name:  "922cee79-843a-4648-813a-56933695d49e",
						Value: "d963d489-ddd5-4119-9256-7b3de2ca4fd9",
					},
				},
			},
		},
		{
			Name: "#2",
			ObjectFunc: func(t *testing.T) metav1.Object {
				t.Helper()

				metadata := metav1.ObjectMeta{
					Name:              "e79b6c20-8d59-46ba-ad1f-ecc5c4c51d35",
					CreationTimestamp: metav1.Date(2026, 1, 2, 10, 25, 30, 0, time.UTC),
					Labels: map[string]string{
						constants.NameLabel: "3f0e0c93-c93e-4e07-98df-09f37c7ade18",
					},
					Annotations: map[string]string{
						constants.DescriptionAnnotation: "dd6eab8f-4a46-4fe1-8fd0-e7cf49107734",
						constants.CreatorAnnotation:     "ecefacdc-755b-4c5d-ac4f-8003eaf2b3eb",
					},
				}

				return NewFakeConditionObject(t, metadata).
					ExpectStatusConditionRead(unikornv1.ConditionAvailable, unikornv1.ConditionReasonProvisioned).
					ExpectStatusConditionRead(unikornv1.ConditionHealthy, unikornv1.ConditionReasonHealthy)
			},
			Tags: nil,
			Expected: openapi.ResourceReadMetadata{
				CreatedBy:          ptr.To("ecefacdc-755b-4c5d-ac4f-8003eaf2b3eb"),
				CreationTime:       time.Date(2026, 1, 2, 10, 25, 30, 0, time.UTC),
				Description:        ptr.To("dd6eab8f-4a46-4fe1-8fd0-e7cf49107734"),
				HealthStatus:       openapi.ResourceHealthStatusHealthy,
				Id:                 "e79b6c20-8d59-46ba-ad1f-ecc5c4c51d35",
				Name:               "3f0e0c93-c93e-4e07-98df-09f37c7ade18",
				ProvisioningStatus: openapi.ResourceProvisioningStatusProvisioned,
			},
		},
	}

	for _, testCase := range testCases {
		t.Run(testCase.Name, func(t *testing.T) {
			t.Parallel()

			object := testCase.ObjectFunc(t)

			metadata := ResourceReadMetadata(object, testCase.Tags)
			require.Equal(t, testCase.Expected, metadata)
		})
	}
}

func TestOrganizationScopedResourceReadMetadata(t *testing.T) {
	t.Parallel()

	expected := openapi.OrganizationScopedResourceReadMetadata{
		HealthStatus:       openapi.ResourceHealthStatusHealthy,
		OrganizationId:     "b4f5c6d7-e8f9-4a0b-9c1d-2e3f4a5b6c7d",
		ProvisioningStatus: openapi.ResourceProvisioningStatusProvisioned,
	}

	metadata := metav1.ObjectMeta{
		Labels: map[string]string{
			constants.OrganizationLabel: "b4f5c6d7-e8f9-4a0b-9c1d-2e3f4a5b6c7d",
		},
	}

	object := NewFakeConditionObject(t, metadata).
		ExpectStatusConditionRead(unikornv1.ConditionAvailable, unikornv1.ConditionReasonProvisioned).
		ExpectStatusConditionRead(unikornv1.ConditionHealthy, unikornv1.ConditionReasonHealthy)

	actual := OrganizationScopedResourceReadMetadata(object, nil)

	require.Equal(t, expected, actual)
}

func TestProjectScopedResourceReadMetadata(t *testing.T) {
	t.Parallel()

	expected := openapi.ProjectScopedResourceReadMetadata{
		HealthStatus:       openapi.ResourceHealthStatusHealthy,
		OrganizationId:     "60709e86-7da5-4cdd-9017-133b73fe2ac8",
		ProjectId:          "0dbb6402-f6c2-46c6-bd81-c1bef71d1746",
		ProvisioningStatus: openapi.ResourceProvisioningStatusProvisioned,
	}

	metadata := metav1.ObjectMeta{
		Labels: map[string]string{
			constants.OrganizationLabel: "60709e86-7da5-4cdd-9017-133b73fe2ac8",
			constants.ProjectLabel:      "0dbb6402-f6c2-46c6-bd81-c1bef71d1746",
		},
	}

	object := NewFakeConditionObject(t, metadata).
		ExpectStatusConditionRead(unikornv1.ConditionAvailable, unikornv1.ConditionReasonProvisioned).
		ExpectStatusConditionRead(unikornv1.ConditionHealthy, unikornv1.ConditionReasonHealthy)

	actual := ProjectScopedResourceReadMetadata(object, nil)

	require.Equal(t, expected, actual)
}

//nolint:paralleltest
func TestNewObjectMetadata(t *testing.T) {
	//nolint:gosec
	uuid.SetRand(rand.New(rand.NewSource(0)))

	t.Cleanup(func() {
		uuid.SetRand(nil)
	})

	type TestCase struct {
		Name      string
		Source    *openapi.ResourceWriteMetadata
		Namespace string
		Expected  *ObjectMetadata
	}

	testCases := []TestCase{
		{
			Name: "metadata without description",
			Source: &openapi.ResourceWriteMetadata{
				Name: "3284e6f1-bd81-4ed3-94e3-878aece5aa1a",
			},
			Namespace: "74948e1c-820d-45e9-bf33-85deea460fe8",
			Expected: &ObjectMetadata{
				Name:      "fb180daf-48a7-4ee0-b10d-394651850fd4",
				Namespace: "74948e1c-820d-45e9-bf33-85deea460fe8",
				Labels: map[string]string{
					constants.NameLabel: "3284e6f1-bd81-4ed3-94e3-878aece5aa1a",
				},
				Annotations: make(map[string]string),
			},
		},
		{
			Name: "metadata with description",
			Source: &openapi.ResourceWriteMetadata{
				Name:        "620f66d7-37c8-4206-8a9b-93aaa1165993",
				Description: ptr.To("d54fa8be-c017-49dc-9443-cb6cf1038794"),
			},
			Namespace: "021d4a12-71e5-49f1-8c78-f5aca89a4e3e",
			Expected: &ObjectMetadata{
				Name:      "a178892e-e285-4ce1-9114-55780875d64e",
				Namespace: "021d4a12-71e5-49f1-8c78-f5aca89a4e3e",
				Labels: map[string]string{
					constants.NameLabel: "620f66d7-37c8-4206-8a9b-93aaa1165993",
				},
				Annotations: map[string]string{
					constants.DescriptionAnnotation: "d54fa8be-c017-49dc-9443-cb6cf1038794",
				},
			},
		},
	}

	for _, testCase := range testCases {
		t.Run(testCase.Name, func(t *testing.T) {
			metadata := NewObjectMetadata(testCase.Source, testCase.Namespace)
			require.Equal(t, testCase.Expected, metadata)
			require.Equal(t, metav1.ObjectMeta(*testCase.Expected), metadata.Get())
		})
	}
}

//nolint:paralleltest,tparallel,dupl
func TestObjectMetadata_WithOrganization(t *testing.T) {
	t.Parallel()

	type TestCase struct {
		Name     string
		Input    *string
		Expected string
	}

	testCases := []TestCase{
		{
			Name:     "returns empty string when organization ID is not set",
			Input:    nil,
			Expected: "",
		},
		{
			Name:     "returns organization ID when it is set",
			Input:    ptr.To("2f07dc1f-2ac0-47f8-af39-3b07b574074e"),
			Expected: "2f07dc1f-2ac0-47f8-af39-3b07b574074e",
		},
		{
			Name:     "returns overridden organization ID when override is provided",
			Input:    ptr.To("6aebfc5c-a008-4c36-a8eb-5348548c3534"),
			Expected: "6aebfc5c-a008-4c36-a8eb-5348548c3534",
		},
		{
			Name:     "returns empty string when organization ID is explicitly set to empty",
			Input:    ptr.To(""),
			Expected: "",
		},
	}

	metadata := ObjectMetadata{
		Labels: make(map[string]string),
	}

	for _, testCase := range testCases {
		t.Run(testCase.Name, func(t *testing.T) {
			if testCase.Input != nil {
				metadata.WithOrganization(*testCase.Input)
			}

			require.Equal(t, testCase.Expected, metadata.Labels[constants.OrganizationLabel])
			require.Equal(t, testCase.Expected, metadata.Get().Labels[constants.OrganizationLabel])
		})
	}
}

//nolint:paralleltest,tparallel,dupl
func TestObjectMetadata_WithProject(t *testing.T) {
	t.Parallel()

	type TestCase struct {
		Name     string
		Input    *string
		Expected string
	}

	testCases := []TestCase{
		{
			Name:     "returns empty string when project ID is not set",
			Input:    nil,
			Expected: "",
		},
		{
			Name:     "returns project ID when it is set",
			Input:    ptr.To("300b5738-1121-4528-9143-b6758662a617"),
			Expected: "300b5738-1121-4528-9143-b6758662a617",
		},
		{
			Name:     "returns overridden project ID when override is provided",
			Input:    ptr.To("9ca2a28a-35d9-4f70-9f67-e998c1143c46"),
			Expected: "9ca2a28a-35d9-4f70-9f67-e998c1143c46",
		},
		{
			Name:     "returns empty string when project ID is explicitly set to empty",
			Input:    ptr.To(""),
			Expected: "",
		},
	}

	metadata := ObjectMetadata{
		Labels: make(map[string]string),
	}

	for _, testCase := range testCases {
		t.Run(testCase.Name, func(t *testing.T) {
			if testCase.Input != nil {
				metadata.WithProject(*testCase.Input)
			}

			require.Equal(t, testCase.Expected, metadata.Labels[constants.ProjectLabel])
			require.Equal(t, testCase.Expected, metadata.Get().Labels[constants.ProjectLabel])
		})
	}
}

//nolint:paralleltest,tparallel
func TestObjectMetadata_WithLabel(t *testing.T) {
	t.Parallel()

	type TestCase struct {
		Name     string
		Key      string
		Value    *string
		Expected map[string]string
	}

	testCases := []TestCase{
		{
			Name:     "returns empty string when key is not set",
			Key:      "e8db7c52-ad6b-4cc0-96b2-2487533ddb38",
			Value:    nil,
			Expected: make(map[string]string),
		},
		{
			Name:  "returns labels containing the provided key and value #1",
			Key:   "e8db7c52-ad6b-4cc0-96b2-2487533ddb38",
			Value: ptr.To("2e942255-8872-4114-811c-d4e594e3b9bb"),
			Expected: map[string]string{
				"e8db7c52-ad6b-4cc0-96b2-2487533ddb38": "2e942255-8872-4114-811c-d4e594e3b9bb",
			},
		},
		{
			Name:  "returns labels containing the provided key and value #2",
			Key:   "a6ace510-9423-45b7-9bf4-f2e797dd818e",
			Value: ptr.To("dbb4dda1-2b18-4d2f-8e12-adbb71e418fd"),
			Expected: map[string]string{
				"e8db7c52-ad6b-4cc0-96b2-2487533ddb38": "2e942255-8872-4114-811c-d4e594e3b9bb",
				"a6ace510-9423-45b7-9bf4-f2e797dd818e": "dbb4dda1-2b18-4d2f-8e12-adbb71e418fd",
			},
		},
		{
			Name:  "returns labels with the provided key and overwritten value",
			Key:   "e8db7c52-ad6b-4cc0-96b2-2487533ddb38",
			Value: ptr.To("5d1d05c6-6683-4bbb-82af-accc1abe61dd"),
			Expected: map[string]string{
				"e8db7c52-ad6b-4cc0-96b2-2487533ddb38": "5d1d05c6-6683-4bbb-82af-accc1abe61dd",
				"a6ace510-9423-45b7-9bf4-f2e797dd818e": "dbb4dda1-2b18-4d2f-8e12-adbb71e418fd",
			},
		},
		{
			Name:  "returns labels with the provided key and an empty value",
			Key:   "e8db7c52-ad6b-4cc0-96b2-2487533ddb38",
			Value: ptr.To(""),
			Expected: map[string]string{
				"e8db7c52-ad6b-4cc0-96b2-2487533ddb38": "",
				"a6ace510-9423-45b7-9bf4-f2e797dd818e": "dbb4dda1-2b18-4d2f-8e12-adbb71e418fd",
			},
		},
	}

	metadata := ObjectMetadata{
		Labels: make(map[string]string),
	}

	for _, testCase := range testCases {
		t.Run(testCase.Name, func(t *testing.T) {
			if testCase.Value != nil {
				metadata.WithLabel(testCase.Key, *testCase.Value)
			}

			require.Equal(t, testCase.Expected, metadata.Labels)
			require.Equal(t, testCase.Expected, metadata.Get().Labels)
		})
	}
}

func TestTagConversion(t *testing.T) {
	t.Parallel()

	unikornTag := unikornv1.Tag{
		Name:  "303ac011-27c5-47b1-aaa5-90d7f322f785",
		Value: "75fe7542-87ed-4918-ac59-7ecba395fcf2",
	}

	openapiTag := openapi.Tag{
		Name:  "303ac011-27c5-47b1-aaa5-90d7f322f785",
		Value: "75fe7542-87ed-4918-ac59-7ecba395fcf2",
	}

	require.Equal(t, openapiTag, ConvertTag(unikornTag))
	require.Equal(t, unikornTag, GenerateTag(openapiTag))
}

func TestTagsConversion(t *testing.T) {
	t.Parallel()

	type TestCase struct {
		Name        string
		UnikornTags unikornv1.TagList
		OpenAPITags *openapi.TagList
	}

	testCases := []TestCase{
		{
			Name:        "#1",
			UnikornTags: nil,
			OpenAPITags: nil,
		},
		{
			Name:        "#2",
			UnikornTags: unikornv1.TagList{},
			OpenAPITags: &openapi.TagList{},
		},
		{
			Name: "#3",
			UnikornTags: unikornv1.TagList{
				{
					Name:  "72668ae7-956e-4f9e-bcc5-373abe9779a8",
					Value: "5451ddff-4718-4179-b221-3fb133afa0f7",
				},
			},
			OpenAPITags: &openapi.TagList{
				{
					Name:  "72668ae7-956e-4f9e-bcc5-373abe9779a8",
					Value: "5451ddff-4718-4179-b221-3fb133afa0f7",
				},
			},
		},
		{
			Name: "#4",
			UnikornTags: unikornv1.TagList{
				{
					Name:  "604e48b0-a38d-463f-a425-5011a03dc33c",
					Value: "26f893e1-311a-44af-8961-c282ab227b3e",
				},
				{
					Name:  "00957e37-0dd9-4d3a-84f7-d1959baad8d7",
					Value: "6864cb7f-1b32-4f1a-b2e9-af69da3224e4",
				},
				{
					Name:  "ecd2528b-7c47-4226-80b1-e5e6b81ad9d8",
					Value: "52c3e645-7a7e-439c-b2a3-f761f13f3207",
				},
			},
			OpenAPITags: &openapi.TagList{
				{
					Name:  "604e48b0-a38d-463f-a425-5011a03dc33c",
					Value: "26f893e1-311a-44af-8961-c282ab227b3e",
				},
				{
					Name:  "00957e37-0dd9-4d3a-84f7-d1959baad8d7",
					Value: "6864cb7f-1b32-4f1a-b2e9-af69da3224e4",
				},
				{
					Name:  "ecd2528b-7c47-4226-80b1-e5e6b81ad9d8",
					Value: "52c3e645-7a7e-439c-b2a3-f761f13f3207",
				},
			},
		},
	}

	for _, testCase := range testCases {
		t.Run(testCase.Name, func(t *testing.T) {
			t.Parallel()

			var openapiTags openapi.TagList
			if testCase.OpenAPITags != nil {
				openapiTags = *testCase.OpenAPITags
			}

			require.Equal(t, openapiTags, ConvertTags(testCase.UnikornTags))
			require.Equal(t, testCase.UnikornTags, GenerateTagList(testCase.OpenAPITags))
		})
	}
}
