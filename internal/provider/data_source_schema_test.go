/*
Copyright 2026 Chainguard, Inc.
SPDX-License-Identifier: Apache-2.0
*/

package provider

import (
	"context"
	"reflect"
	"strings"
	"testing"

	"github.com/hashicorp/terraform-plugin-framework/datasource"
	"github.com/hashicorp/terraform-plugin-framework/datasource/schema"
)

// schemaModelTestCase defines a test case for validating schema/model
// consistency.
type schemaModelTestCase struct {
	name       string
	dataSource datasource.DataSource
	// topLevelModel is the top-level data source model struct.
	topLevelModel any
	// nestedItems are nested list items to validate.
	nestedItems []nestedItemTestCase
	// singleNested are single nested attributes to validate.
	singleNested []singleNestedTestCase
	// mapNested are map nested attributes to validate.
	mapNested []mapNestedTestCase
	// skipTopLevel skips top-level validation (for complex cases).
	skipTopLevel bool
}

// nestedItemTestCase defines a nested list attribute and its model.
type nestedItemTestCase struct {
	// attrPath is the path to the nested attribute (e.g., ["items"]).
	attrPath []string
	// model is the model struct for items in the list.
	model any
}

// singleNestedTestCase defines a single nested attribute and its model.
type singleNestedTestCase struct {
	// attrPath is the path to the nested attribute.
	attrPath []string
	// model is the model struct for the nested object.
	model any
}

// mapNestedTestCase defines a map nested attribute and its model.
type mapNestedTestCase struct {
	// attrPath is the path to the nested attribute.
	attrPath []string
	// model is the model struct for values in the map.
	model any
}

// TestAllDataSources_SchemaMatchesModel validates that all data source schemas
// match their corresponding model structs. This prevents runtime errors when
// Terraform tries to unmarshal data into models with missing schema attributes.
func TestAllDataSources_SchemaMatchesModel(t *testing.T) {
	testCases := []schemaModelTestCase{
		{
			name:          "chainguard_group",
			dataSource:    NewGroupDataSource(),
			topLevelModel: groupDataSourceModel{},
		},
		{
			name:          "chainguard_identity",
			dataSource:    NewIdentityDataSource(),
			topLevelModel: identityDataSourceModel{},
		},
		{
			name:          "chainguard_role",
			dataSource:    NewRoleDataSource(),
			topLevelModel: roleDataSourceModel{},
			nestedItems: []nestedItemTestCase{
				{attrPath: []string{"items"}, model: roleModel{}},
			},
		},
		{
			name:          "chainguard_image_repo",
			dataSource:    NewImageRepoDataSource(),
			topLevelModel: imageRepoDataSourceModel{},
			nestedItems: []nestedItemTestCase{
				{attrPath: []string{"items"}, model: imageRepoModel{}},
			},
		},
		{
			name:          "chainguard_image_repos",
			dataSource:    NewImageReposDataSource(),
			topLevelModel: imageReposDataSourceModel{},
			nestedItems: []nestedItemTestCase{
				{attrPath: []string{"items"}, model: imageRepoModel{}},
			},
		},
		{
			name:          "chainguard_versions",
			dataSource:    NewVersionsDataSource(),
			topLevelModel: versionsDataSourceModel{},
			singleNested: []singleNestedTestCase{
				{attrPath: []string{"versions"}, model: versionsDataSourceProtoModel{}},
			},
			nestedItems: []nestedItemTestCase{
				{attrPath: []string{"versions", "eol_versions"}, model: versionsDataSourceProtoEolVersionsModel{}},
				{attrPath: []string{"versions", "versions"}, model: versionsDataSourceProtoVersionsModel{}},
			},
			mapNested: []mapNestedTestCase{
				{attrPath: []string{"version_map"}, model: versionsDataSourceVersionMapModel{}},
			},
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			ctx := context.Background()
			req := datasource.SchemaRequest{}
			resp := &datasource.SchemaResponse{}
			tc.dataSource.Schema(ctx, req, resp)

			if resp.Diagnostics.HasError() {
				t.Fatalf("Schema() returned errors: %v", resp.Diagnostics.Errors())
			}

			// Validate top-level model
			if !tc.skipTopLevel {
				missingInSchema := validateModelAgainstSchema(tc.topLevelModel, resp.Schema.Attributes)
				if len(missingInSchema) > 0 {
					t.Errorf("top-level schema/model mismatch: model has fields missing from schema: %v", missingInSchema)
				}
			}

			// Validate nested list items
			for _, nested := range tc.nestedItems {
				schemaAttrs, err := getNestedListAttributes(resp.Schema.Attributes, nested.attrPath)
				if err != nil {
					t.Errorf("failed to get nested list attributes at path %v: %v", nested.attrPath, err)
					continue
				}

				missingInSchema := validateModelAgainstSchema(nested.model, schemaAttrs)
				if len(missingInSchema) > 0 {
					t.Errorf("nested list schema/model mismatch at path %v: model has fields missing from schema: %v",
						nested.attrPath, missingInSchema)
				}
			}

			// Validate single nested attributes
			for _, nested := range tc.singleNested {
				schemaAttrs, err := getSingleNestedAttributes(resp.Schema.Attributes, nested.attrPath)
				if err != nil {
					t.Errorf("failed to get single nested attributes at path %v: %v", nested.attrPath, err)
					continue
				}

				missingInSchema := validateModelAgainstSchema(nested.model, schemaAttrs)
				if len(missingInSchema) > 0 {
					t.Errorf("single nested schema/model mismatch at path %v: model has fields missing from schema: %v",
						nested.attrPath, missingInSchema)
				}
			}

			// Validate map nested attributes
			for _, nested := range tc.mapNested {
				schemaAttrs, err := getMapNestedAttributes(resp.Schema.Attributes, nested.attrPath)
				if err != nil {
					t.Errorf("failed to get map nested attributes at path %v: %v", nested.attrPath, err)
					continue
				}

				missingInSchema := validateModelAgainstSchema(nested.model, schemaAttrs)
				if len(missingInSchema) > 0 {
					t.Errorf("map nested schema/model mismatch at path %v: model has fields missing from schema: %v",
						nested.attrPath, missingInSchema)
				}
			}
		})
	}
}

// validateModelAgainstSchema checks that all tfsdk-tagged fields in the model
// have corresponding attributes in the schema.
func validateModelAgainstSchema(model any, schemaAttrs map[string]schema.Attribute) []string {
	modelType := reflect.TypeOf(model)
	if modelType.Kind() == reflect.Ptr {
		modelType = modelType.Elem()
	}

	var missingInSchema []string
	for i := 0; i < modelType.NumField(); i++ {
		field := modelType.Field(i)
		tfsdkTag := field.Tag.Get("tfsdk")
		if tfsdkTag == "" || tfsdkTag == "-" {
			continue
		}

		if _, exists := schemaAttrs[tfsdkTag]; !exists {
			missingInSchema = append(missingInSchema, tfsdkTag)
		}
	}

	return missingInSchema
}

// getNestedListAttributes navigates to a nested list attribute and returns its attributes.
func getNestedListAttributes(attrs map[string]schema.Attribute, path []string) (map[string]schema.Attribute, error) {
	if len(path) == 0 {
		return nil, nil
	}

	attr, ok := attrs[path[0]]
	if !ok {
		return nil, &pathError{path: path, msg: "attribute not found"}
	}

	// If this is the last element, get the list nested attributes
	if len(path) == 1 {
		listNested, ok := attr.(schema.ListNestedAttribute)
		if !ok {
			return nil, &pathError{path: path, msg: "attribute is not ListNestedAttribute"}
		}
		return listNested.NestedObject.Attributes, nil
	}

	// Otherwise, navigate deeper through single nested or list nested
	switch typedAttr := attr.(type) {
	case schema.SingleNestedAttribute:
		return getNestedListAttributes(typedAttr.Attributes, path[1:])
	case schema.ListNestedAttribute:
		return getNestedListAttributes(typedAttr.NestedObject.Attributes, path[1:])
	default:
		return nil, &pathError{path: path, msg: "cannot navigate through this attribute type"}
	}
}

// getSingleNestedAttributes navigates to a single nested attribute and returns its attributes.
func getSingleNestedAttributes(attrs map[string]schema.Attribute, path []string) (map[string]schema.Attribute, error) {
	if len(path) == 0 {
		return nil, nil
	}

	attr, ok := attrs[path[0]]
	if !ok {
		return nil, &pathError{path: path, msg: "attribute not found"}
	}

	// If this is the last element, get the single nested attributes
	if len(path) == 1 {
		singleNested, ok := attr.(schema.SingleNestedAttribute)
		if !ok {
			return nil, &pathError{path: path, msg: "attribute is not SingleNestedAttribute"}
		}
		return singleNested.Attributes, nil
	}

	// Otherwise, navigate deeper
	switch typedAttr := attr.(type) {
	case schema.SingleNestedAttribute:
		return getSingleNestedAttributes(typedAttr.Attributes, path[1:])
	default:
		return nil, &pathError{path: path, msg: "cannot navigate through this attribute type"}
	}
}

// getMapNestedAttributes navigates to a map nested attribute and returns its attributes.
func getMapNestedAttributes(attrs map[string]schema.Attribute, path []string) (map[string]schema.Attribute, error) {
	if len(path) == 0 {
		return nil, nil
	}

	attr, ok := attrs[path[0]]
	if !ok {
		return nil, &pathError{path: path, msg: "attribute not found"}
	}

	// If this is the last element, get the map nested attributes
	if len(path) == 1 {
		mapNested, ok := attr.(schema.MapNestedAttribute)
		if !ok {
			return nil, &pathError{path: path, msg: "attribute is not MapNestedAttribute"}
		}
		return mapNested.NestedObject.Attributes, nil
	}

	// Otherwise, navigate deeper
	switch typedAttr := attr.(type) {
	case schema.SingleNestedAttribute:
		return getMapNestedAttributes(typedAttr.Attributes, path[1:])
	default:
		return nil, &pathError{path: path, msg: "cannot navigate through this attribute type"}
	}
}

// pathError represents an error navigating to a schema path.
type pathError struct {
	path []string
	msg  string
}

func (e *pathError) Error() string {
	return e.msg + " at path " + joinPath(e.path)
}

func joinPath(path []string) string {
	if len(path) == 0 {
		return "(root)"
	}
	return strings.Join(path, ".")
}
