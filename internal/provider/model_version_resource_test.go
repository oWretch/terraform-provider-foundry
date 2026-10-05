package provider

import (
	"context"
	"testing"

	"github.com/hashicorp/terraform-plugin-framework/datasource"
	"github.com/hashicorp/terraform-plugin-framework/resource"

	"github.com/oWretch/terraform-provider-foundry/internal/clients"
)

func TestModelVersionRequiresPreviewFeature(t *testing.T) {
	t.Parallel()

	client, err := clients.New(clients.Config{
		AccountName: "example",
		ProjectName: "demo",
		Environment: clients.EnvironmentPublic,
		Auth:        clients.Authentication{Method: clients.AuthenticationAPIKey, APIKey: "secret"},
	})
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}

	resourceUnderTest := NewModelVersionResource().(*modelVersionResource)
	resourceUnderTest.client = client
	var resourceResponse resource.ValidateConfigResponse
	resourceUnderTest.ValidateConfig(context.Background(), resource.ValidateConfigRequest{}, &resourceResponse)
	if !resourceResponse.Diagnostics.HasError() {
		t.Fatal("expected the model version resource to require the models preview feature")
	}

	dataSourceUnderTest := NewModelVersionDataSource().(*modelVersionDataSource)
	dataSourceUnderTest.client = client
	var dataSourceResponse datasource.ValidateConfigResponse
	dataSourceUnderTest.ValidateConfig(context.Background(), datasource.ValidateConfigRequest{}, &dataSourceResponse)
	if !dataSourceResponse.Diagnostics.HasError() {
		t.Fatal("expected the model version data source to require the models preview feature")
	}
}

func TestModelVersionAllowsPreviewFeature(t *testing.T) {
	t.Parallel()

	client, err := clients.New(clients.Config{
		AccountName:     "example",
		ProjectName:     "demo",
		Environment:     clients.EnvironmentPublic,
		PreviewFeatures: []string{modelsPreviewFeatureName},
		Auth:            clients.Authentication{Method: clients.AuthenticationAPIKey, APIKey: "secret"},
	})
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}

	resourceUnderTest := NewModelVersionResource().(*modelVersionResource)
	resourceUnderTest.client = client
	var response resource.ValidateConfigResponse
	resourceUnderTest.ValidateConfig(context.Background(), resource.ValidateConfigRequest{}, &response)
	if response.Diagnostics.HasError() {
		t.Fatalf("unexpected preview validation error: %v", response.Diagnostics)
	}
}

func TestEscapeBlobPath(t *testing.T) {
	cases := []struct {
		name string
		in   string
		want string
	}{
		{"plain", "model.bin", "model.bin"},
		{"nested path keeps separators", "weights/shard-1.safetensors", "weights/shard-1.safetensors"},
		{"space is escaped", "my model.bin", "my%20model.bin"},
		{"hash is escaped", "model#1.bin", "model%231.bin"},
		{"question mark is escaped", "model?.bin", "model%3F.bin"},
		{"percent is escaped", "100%.bin", "100%25.bin"},
		{"nested path with spaces", "sub dir/my model.bin", "sub%20dir/my%20model.bin"},
	}

	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			if got := escapeBlobPath(testCase.in); got != testCase.want {
				t.Fatalf("escapeBlobPath(%q) = %q, want %q", testCase.in, got, testCase.want)
			}
		})
	}
}
