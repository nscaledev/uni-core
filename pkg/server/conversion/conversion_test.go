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
	"bytes"
	"encoding/json"
	"math/rand"
	"testing"
	"testing/synctest"
	"time"

	"github.com/go-logr/logr"
	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
	"go.uber.org/mock/gomock"

	unikornv1 "github.com/unikorn-cloud/core/pkg/apis/unikorn/v1alpha1"
	mockv1 "github.com/unikorn-cloud/core/pkg/apis/unikorn/v1alpha1/mock"
	"github.com/unikorn-cloud/core/pkg/constants"
	"github.com/unikorn-cloud/core/pkg/openapi"
	"github.com/unikorn-cloud/core/pkg/util/testutil"

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

func MetadataFailer(newObject, oldObject metav1.Object) error {
	return testutil.ErrMustFail
}

func MetadataCopier(newObject, oldObject metav1.Object) error {
	oldAnnotations := oldObject.GetAnnotations()
	if oldAnnotations == nil {
		return nil
	}

	newAnnotations := newObject.GetAnnotations()
	if newAnnotations == nil {
		newAnnotations = make(map[string]string)
	}

	for key, value := range oldAnnotations {
		newAnnotations[key] = value
	}

	newObject.SetAnnotations(newAnnotations)

	return nil
}

func MetadataWriter(key, value string) MetadataMutationFunc {
	return func(newObject, oldObject metav1.Object) error {
		annotations := newObject.GetAnnotations()
		if annotations == nil {
			annotations = make(map[string]string)
		}

		annotations[key] = value

		newObject.SetAnnotations(annotations)

		return nil
	}
}

func TestUpdateObjectMetadata(t *testing.T) {
	t.Parallel()

	syncTestTime := time.Date(2000, 1, 1, 0, 0, 0, 0, time.UTC)

	type TestCase struct {
		Name         string
		ClockAdvance time.Duration
		NewObject    metav1.Object
		OldObject    metav1.Object
		Mutators     []MetadataMutationFunc
		ExpectError  bool
		Expected     *metav1.ObjectMeta
	}

	testCases := []TestCase{
		{
			Name:         "modifies object without mutators provided",
			ClockAdvance: 5 * time.Minute,
			NewObject: &metav1.ObjectMeta{
				Annotations: map[string]string{
					"0450f2dc-d17b-4720-a476-3825d6f49f21": "a6c08a50-4ff6-4b1c-8b3b-13f7918b0f43",
					"09a1e622-4006-4228-bc15-e130655b106a": "06ba5d6b-173c-4894-b092-0ae89ed8239e",
				},
			},
			OldObject: &metav1.ObjectMeta{
				Annotations: map[string]string{
					"b60df99c-96f6-4ee3-b7dc-4907cad731bc": "f578d1bb-a016-402c-9d10-a314251d3c94",
				},
			},
			Mutators:    nil,
			ExpectError: false,
			Expected: &metav1.ObjectMeta{
				Annotations: map[string]string{
					"0450f2dc-d17b-4720-a476-3825d6f49f21": "a6c08a50-4ff6-4b1c-8b3b-13f7918b0f43",
					"09a1e622-4006-4228-bc15-e130655b106a": "06ba5d6b-173c-4894-b092-0ae89ed8239e",
					constants.ModifiedTimestampAnnotation:  syncTestTime.Add(5 * time.Minute).Format(time.RFC3339),
				},
			},
		},
		{
			Name:         "returns error when mutators fail #1",
			ClockAdvance: 15 * time.Minute,
			NewObject:    &metav1.ObjectMeta{},
			OldObject:    &metav1.ObjectMeta{},
			Mutators: []MetadataMutationFunc{
				MetadataFailer,
			},
			ExpectError: true,
			Expected: &metav1.ObjectMeta{
				Annotations: map[string]string{
					constants.ModifiedTimestampAnnotation: syncTestTime.Add(15 * time.Minute).Format(time.RFC3339),
				},
			},
		},
		{
			Name:         "returns error when mutators fail #2",
			ClockAdvance: time.Hour,
			NewObject: &metav1.ObjectMeta{
				Annotations: map[string]string{
					"0db34c23-9086-4a83-b361-88235f9b3c8f": "ccb60c33-a987-44f3-9930-79edefa91503",
					"9ab3c9fa-1e80-4eb8-ba58-bb5fbf654dae": "5e0bb38a-8232-4a69-aa31-c090b1afe8f0",
				},
			},
			OldObject: &metav1.ObjectMeta{
				Annotations: map[string]string{
					"3dfb4381-81a1-4d3d-aef9-2e1786241c35": "abf65c69-3246-4255-a083-49f68f5145ad",
					"0db34c23-9086-4a83-b361-88235f9b3c8f": "3be77e13-fbb2-4a1b-b71e-f1c124637515",
					"86e94588-7d33-480f-939f-58d10b76d3ed": "f977e892-0ba9-4bb5-a4b7-5a40aed6de2e",
				},
			},
			Mutators: []MetadataMutationFunc{
				MetadataCopier,
				MetadataFailer,
			},
			ExpectError: true,
			Expected: &metav1.ObjectMeta{
				Annotations: map[string]string{
					"0db34c23-9086-4a83-b361-88235f9b3c8f": "3be77e13-fbb2-4a1b-b71e-f1c124637515",
					"9ab3c9fa-1e80-4eb8-ba58-bb5fbf654dae": "5e0bb38a-8232-4a69-aa31-c090b1afe8f0",
					"3dfb4381-81a1-4d3d-aef9-2e1786241c35": "abf65c69-3246-4255-a083-49f68f5145ad",
					"86e94588-7d33-480f-939f-58d10b76d3ed": "f977e892-0ba9-4bb5-a4b7-5a40aed6de2e",
					constants.ModifiedTimestampAnnotation:  syncTestTime.Add(time.Hour).Format(time.RFC3339),
				},
			},
		},
		{
			Name:         "modifies error when mutators fail #3",
			ClockAdvance: 24 * time.Hour,
			NewObject:    &metav1.ObjectMeta{},
			OldObject: &metav1.ObjectMeta{
				Annotations: map[string]string{
					"5a1e620b-5b03-4c26-a13b-f7469ce65fd9": "bad580e8-ea86-4e29-bd37-da623897f525",
					"f7b0c84d-3471-4c22-9a4a-47b73f7a05ff": "930a6532-77c1-4d88-92cf-76555d4fa36f",
				},
			},
			Mutators: []MetadataMutationFunc{
				MetadataCopier,
				MetadataWriter("b76999f8-daeb-42b9-aa7b-9e89cedd61d6", "c02ebc17-57bd-47fa-9297-d675ab5ff5af"),
				MetadataFailer,
			},
			ExpectError: true,
			Expected: &metav1.ObjectMeta{
				Annotations: map[string]string{
					"5a1e620b-5b03-4c26-a13b-f7469ce65fd9": "bad580e8-ea86-4e29-bd37-da623897f525",
					"f7b0c84d-3471-4c22-9a4a-47b73f7a05ff": "930a6532-77c1-4d88-92cf-76555d4fa36f",
					"b76999f8-daeb-42b9-aa7b-9e89cedd61d6": "c02ebc17-57bd-47fa-9297-d675ab5ff5af",
					constants.ModifiedTimestampAnnotation:  syncTestTime.Add(24 * time.Hour).Format(time.RFC3339),
				},
			},
		},
		{
			Name:         "modifies object with mutators provided #1",
			ClockAdvance: 1825 * time.Minute,
			NewObject:    &metav1.ObjectMeta{},
			OldObject: &metav1.ObjectMeta{
				Annotations: map[string]string{
					"312cd629-16ed-4edd-8825-004864253d7e": "d6359567-d162-4788-a741-8da712744e40",
				},
			},
			Mutators: []MetadataMutationFunc{
				MetadataCopier,
			},
			ExpectError: false,
			Expected: &metav1.ObjectMeta{
				Annotations: map[string]string{
					"312cd629-16ed-4edd-8825-004864253d7e": "d6359567-d162-4788-a741-8da712744e40",
					constants.ModifiedTimestampAnnotation:  syncTestTime.Add(1825 * time.Minute).Format(time.RFC3339),
				},
			},
		},
		{
			Name:         "modifies object with mutators provided #2",
			ClockAdvance: 19782 * time.Second,
			NewObject: &metav1.ObjectMeta{
				Annotations: map[string]string{
					"6bf108ab-cf6a-4b45-bc87-1a22b6700fa5": "893594ce-af4d-4ac9-abe2-918ee7cb663a",
				},
			},
			OldObject: &metav1.ObjectMeta{},
			Mutators: []MetadataMutationFunc{
				MetadataCopier,
				MetadataWriter("6bf108ab-cf6a-4b45-bc87-1a22b6700fa5", "58541ef3-64a4-4adc-863e-fd3b8b8a6242"),
			},
			ExpectError: false,
			Expected: &metav1.ObjectMeta{
				Annotations: map[string]string{
					"6bf108ab-cf6a-4b45-bc87-1a22b6700fa5": "58541ef3-64a4-4adc-863e-fd3b8b8a6242",
					constants.ModifiedTimestampAnnotation:  syncTestTime.Add(19782 * time.Second).Format(time.RFC3339),
				},
			},
		},
		{
			Name:         "modifies object with mutators provided #3",
			ClockAdvance: 78 * time.Minute,
			NewObject: &metav1.ObjectMeta{
				Annotations: map[string]string{
					"87b5dcba-b21f-480e-ae21-2a7d510a739f": "346dee50-0bcd-4c6b-9722-d1b610ed148c",
					"75dbc78e-d25d-46da-bf18-15f7c8d44c07": "060c9ee8-1313-4e03-b63d-0f2691c42ce5",
				},
			},
			OldObject: &metav1.ObjectMeta{
				Annotations: map[string]string{
					"76e7f76f-c83b-46ef-ac86-41523e37462a": "36b38385-412e-4272-9914-214bebb10e50",
				},
			},
			Mutators: []MetadataMutationFunc{
				MetadataWriter("312e1903-39c4-4e8b-8055-3cf0370d1e32", "a82ee897-67c9-4566-b338-c0fd5336f5f9"),
				MetadataWriter("ce3dea09-2c17-4d7c-b644-ce9a4904d3f0", "fb9f8314-6e64-4ca9-a08c-089fbf8054ad"),
				MetadataWriter("75dbc78e-d25d-46da-bf18-15f7c8d44c07", "48cebc9f-6a3e-491d-a1b0-5ba322640925"),
			},
			ExpectError: false,
			Expected: &metav1.ObjectMeta{
				Annotations: map[string]string{
					"87b5dcba-b21f-480e-ae21-2a7d510a739f": "346dee50-0bcd-4c6b-9722-d1b610ed148c",
					"75dbc78e-d25d-46da-bf18-15f7c8d44c07": "48cebc9f-6a3e-491d-a1b0-5ba322640925",
					"312e1903-39c4-4e8b-8055-3cf0370d1e32": "a82ee897-67c9-4566-b338-c0fd5336f5f9",
					"ce3dea09-2c17-4d7c-b644-ce9a4904d3f0": "fb9f8314-6e64-4ca9-a08c-089fbf8054ad",
					constants.ModifiedTimestampAnnotation:  syncTestTime.Add(78 * time.Minute).Format(time.RFC3339),
				},
			},
		},
	}

	for _, testCase := range testCases {
		t.Run(testCase.Name, func(t *testing.T) {
			t.Parallel()

			synctest.Test(t, func(t *testing.T) {
				t.Helper()

				time.Sleep(testCase.ClockAdvance)

				err := UpdateObjectMetadata(testCase.NewObject, testCase.OldObject, testCase.Mutators...)
				require.Equal(t, testCase.ExpectError, err != nil)
				require.Equal(t, testCase.Expected, testCase.NewObject)
			})
		})
	}
}

type FakeLogSink struct {
	t *testing.T
	b *bytes.Buffer
}

func NewFakeLogSink(t *testing.T) *FakeLogSink {
	t.Helper()

	return &FakeLogSink{
		t: t,
		b: bytes.NewBuffer(nil),
	}
}

func (s *FakeLogSink) Init(info logr.RuntimeInfo) {}

func (s *FakeLogSink) Enabled(level int) bool {
	return true
}

func (s *FakeLogSink) Info(level int, msg string, keysAndValues ...any) {
	type Line struct {
		Message string `json:"message"`
		Pairs   []any  `json:"pairs"`
	}

	line := Line{
		Message: msg,
		Pairs:   keysAndValues,
	}

	data, err := json.Marshal(line)
	require.NoError(s.t, err)

	_, err = s.b.Write(data)
	require.NoError(s.t, err)
}

func (s *FakeLogSink) Error(err error, msg string, keysAndValues ...any) {
	panic("implement me")
}

func (s *FakeLogSink) WithValues(keysAndValues ...any) logr.LogSink {
	return s
}

func (s *FakeLogSink) WithName(name string) logr.LogSink {
	return s
}

type UnmarshalableObject struct {
	metav1.ObjectMeta
}

func (u UnmarshalableObject) MarshalJSON() ([]byte, error) {
	return nil, testutil.ErrMustFail
}

type JSONArrayObject struct {
	metav1.ObjectMeta
}

func (j JSONArrayObject) MarshalJSON() ([]byte, error) {
	return []byte("[]"), nil
}

func TestLogUpdate(t *testing.T) {
	t.Parallel()

	type TestCase struct {
		Name         string
		NewObject    metav1.Object
		OldObject    metav1.Object
		ExpectError  bool
		ExpectedLine string
	}

	testCases := []TestCase{
		{
			Name:         "returns error when new object is not marshalable",
			NewObject:    &UnmarshalableObject{},
			OldObject:    &metav1.ObjectMeta{},
			ExpectError:  true,
			ExpectedLine: "",
		},
		{
			Name:         "returns error when old object is not marshalable",
			NewObject:    &metav1.ObjectMeta{},
			OldObject:    &UnmarshalableObject{},
			ExpectError:  true,
			ExpectedLine: "",
		},
		{
			Name:         "returns error when objects are not comparable",
			NewObject:    &metav1.ObjectMeta{},
			OldObject:    &JSONArrayObject{},
			ExpectError:  true,
			ExpectedLine: "",
		},
		{
			Name: "logs no changes when objects are equal",
			NewObject: &metav1.ObjectMeta{
				Name:              "4bc0813b-2bed-4766-87bd-e18261a4ed28",
				Namespace:         "6207c5d6-16d9-47d1-963f-9c5a25ad7114",
				UID:               "1bc7abab-ebaa-41f9-8e9f-248c9f420198",
				ResourceVersion:   "112938",
				Generation:        32,
				CreationTimestamp: metav1.Date(2026, 1, 1, 9, 0, 0, 0, time.UTC),
				DeletionTimestamp: ptr.To(metav1.Date(2026, 1, 12, 11, 10, 0, 0, time.UTC)),
				Labels: map[string]string{
					"e5666dee-5be5-4853-8c71-8a7e3e96ca2d": "83f0ab5d-6236-4f5b-aff3-81a2dedadc94",
					"22727517-c4e9-437c-bc63-0467157e3df5": "cfc976eb-5d69-4aa8-a8c2-b5538d8943a0",
				},
				Finalizers: []string{
					"7a971702-ff39-45fd-a7d4-48f44841ab65",
					"b00b11bf-b275-49d9-a3d5-1da3ac27a0e5",
				},
			},
			OldObject: &metav1.ObjectMeta{
				Name:              "4bc0813b-2bed-4766-87bd-e18261a4ed28",
				Namespace:         "6207c5d6-16d9-47d1-963f-9c5a25ad7114",
				UID:               "1bc7abab-ebaa-41f9-8e9f-248c9f420198",
				ResourceVersion:   "112938",
				Generation:        32,
				CreationTimestamp: metav1.Date(2026, 1, 1, 9, 0, 0, 0, time.UTC),
				DeletionTimestamp: ptr.To(metav1.Date(2026, 1, 12, 11, 10, 0, 0, time.UTC)),
				Labels: map[string]string{
					"e5666dee-5be5-4853-8c71-8a7e3e96ca2d": "83f0ab5d-6236-4f5b-aff3-81a2dedadc94",
					"22727517-c4e9-437c-bc63-0467157e3df5": "cfc976eb-5d69-4aa8-a8c2-b5538d8943a0",
				},
				Finalizers: []string{
					"7a971702-ff39-45fd-a7d4-48f44841ab65",
					"b00b11bf-b275-49d9-a3d5-1da3ac27a0e5",
				},
			},
			ExpectError:  false,
			ExpectedLine: `{"message":"patching resource","pairs":["current",{"creationTimestamp":"2026-01-01T09:00:00Z","deletionTimestamp":"2026-01-12T11:10:00Z","finalizers":["7a971702-ff39-45fd-a7d4-48f44841ab65","b00b11bf-b275-49d9-a3d5-1da3ac27a0e5"],"generation":32,"labels":{"22727517-c4e9-437c-bc63-0467157e3df5":"cfc976eb-5d69-4aa8-a8c2-b5538d8943a0","e5666dee-5be5-4853-8c71-8a7e3e96ca2d":"83f0ab5d-6236-4f5b-aff3-81a2dedadc94"},"name":"4bc0813b-2bed-4766-87bd-e18261a4ed28","namespace":"6207c5d6-16d9-47d1-963f-9c5a25ad7114","resourceVersion":"112938","uid":"1bc7abab-ebaa-41f9-8e9f-248c9f420198"},"patch",{}]}`,
		},
		{
			Name: "logs changes when objects differ",
			NewObject: &metav1.ObjectMeta{
				Name:              "f9ae4604-668a-4a30-8256-2502c9cd4fd8",
				Namespace:         "6168cfb5-d6a0-4663-9ec6-d533c1e45a74",
				UID:               "28da1c0f-1d59-4c9d-9378-89f69bfbd04f",
				ResourceVersion:   "213864",
				Generation:        14,
				CreationTimestamp: metav1.Date(2026, 1, 1, 7, 30, 0, 0, time.UTC),
				Labels: map[string]string{
					"0a7c2e8e-819a-47f7-958f-f5b4215cc6d6": "a7970518-d3cc-4b2a-a1f0-0149c248aab3",
				},
				Finalizers: []string{"88e26d82-e910-48cc-92b8-cbc2be1008d6"},
			},
			OldObject: &metav1.ObjectMeta{
				Name:              "f9ae4604-668a-4a30-8256-2502c9cd4fd8",
				Namespace:         "6168cfb5-d6a0-4663-9ec6-d533c1e45a74",
				UID:               "28da1c0f-1d59-4c9d-9378-89f69bfbd04f",
				ResourceVersion:   "213879",
				Generation:        17,
				CreationTimestamp: metav1.Date(2026, 1, 1, 7, 30, 0, 0, time.UTC),
				DeletionTimestamp: ptr.To(metav1.Date(2026, 1, 12, 11, 36, 0, 0, time.UTC)),
				Labels: map[string]string{
					"0a7c2e8e-819a-47f7-958f-f5b4215cc6d6": "a7970518-d3cc-4b2a-a1f0-0149c248aab3",
				},
				Finalizers: []string{
					"88e26d82-e910-48cc-92b8-cbc2be1008d6",
					"c883acc5-73a5-49e4-a1c4-e15893f58b2d",
				},
			},
			ExpectError:  false,
			ExpectedLine: `{"message":"patching resource","pairs":["current",{"creationTimestamp":"2026-01-01T07:30:00Z","finalizers":["88e26d82-e910-48cc-92b8-cbc2be1008d6"],"generation":14,"labels":{"0a7c2e8e-819a-47f7-958f-f5b4215cc6d6":"a7970518-d3cc-4b2a-a1f0-0149c248aab3"},"name":"f9ae4604-668a-4a30-8256-2502c9cd4fd8","namespace":"6168cfb5-d6a0-4663-9ec6-d533c1e45a74","resourceVersion":"213864","uid":"28da1c0f-1d59-4c9d-9378-89f69bfbd04f"},"patch",{"deletionTimestamp":"2026-01-12T11:36:00Z","finalizers":["88e26d82-e910-48cc-92b8-cbc2be1008d6","c883acc5-73a5-49e4-a1c4-e15893f58b2d"],"generation":17,"resourceVersion":"213879"}]}`,
		},
	}

	for _, testCase := range testCases {
		t.Run(testCase.Name, func(t *testing.T) {
			t.Parallel()

			var (
				sink   = NewFakeLogSink(t)
				logger = logr.New(sink)
				ctx    = logr.NewContext(t.Context(), logger)
			)

			err := LogUpdate(ctx, testCase.NewObject, testCase.OldObject)
			require.Equal(t, testCase.ExpectError, err != nil)
			require.Equal(t, testCase.ExpectedLine, sink.b.String())
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
