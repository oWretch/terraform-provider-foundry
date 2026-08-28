package provider

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"

	"github.com/hashicorp/terraform-plugin-framework/diag"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/planmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/stringplanmodifier"
	"github.com/hashicorp/terraform-plugin-framework/types"

	"github.com/oWretch/terraform-provider-foundry/internal/clients"
)

// agentCommon holds the attributes shared by every agent kind. Each kind embeds
// this and contributes only its own definition fields.
type agentCommon struct {
	Name        types.String `tfsdk:"name"`
	Description types.String `tfsdk:"description"`
	ID          types.String `tfsdk:"id"`
	Version     types.String `tfsdk:"version"`
	AgentGUID   types.String `tfsdk:"agent_guid"`
	PrincipalID types.String `tfsdk:"principal_id"`
	ClientID    types.String `tfsdk:"client_id"`
}

// agentCommonSchema returns the attributes every agent resource exposes.
func agentCommonSchema() map[string]schema.Attribute {
	return map[string]schema.Attribute{
		"name": schema.StringAttribute{
			Required:            true,
			MarkdownDescription: "Agent name. Changing this forces a new agent to be created.",
			PlanModifiers:       []planmodifier.String{stringplanmodifier.RequiresReplace()},
		},
		"description": schema.StringAttribute{
			Optional:            true,
			MarkdownDescription: "Description of the agent version.",
		},
		"id": schema.StringAttribute{
			Computed:            true,
			MarkdownDescription: "Identifier of the current agent version, in `name:version` form.",
		},
		"version": schema.StringAttribute{
			Computed:            true,
			MarkdownDescription: "Current agent version number.",
		},
		"agent_guid": schema.StringAttribute{
			Computed:            true,
			MarkdownDescription: "Service-assigned unique identifier for the agent.",
		},
		"principal_id": schema.StringAttribute{
			Computed:            true,
			MarkdownDescription: "Principal ID of the agent instance identity.",
		},
		"client_id": schema.StringAttribute{
			Computed:            true,
			MarkdownDescription: "Client ID of the agent instance identity.",
		},
	}
}

type agentIdentity struct {
	PrincipalID string `json:"principal_id"`
	ClientID    string `json:"client_id"`
}

// agentVersion is the service representation of a single immutable agent version.
// Definition stays raw so each kind can decode only the fields it models.
type agentVersion struct {
	ID          string          `json:"id"`
	Version     string          `json:"version"`
	Description string          `json:"description"`
	Definition  json.RawMessage `json:"definition"`
	AgentGUID   string          `json:"agent_guid"`
	Identity    *agentIdentity  `json:"instance_identity"`
}

type agentResponse struct {
	Versions struct {
		Latest agentVersion `json:"latest"`
	} `json:"versions"`
}

type agentRequest struct {
	Name        string `json:"name,omitempty"`
	Description string `json:"description,omitempty"`
	Definition  any    `json:"definition"`
}

// applyVersion copies the service-assigned fields of version into the common attributes.
func (c *agentCommon) applyVersion(version agentVersion) {
	c.ID = types.StringValue(version.ID)
	c.Version = types.StringValue(version.Version)
	c.AgentGUID = types.StringValue(version.AgentGUID)
	c.Description = optionalString(version.Description)
	if version.Identity != nil {
		c.PrincipalID = types.StringValue(version.Identity.PrincipalID)
		c.ClientID = types.StringValue(version.Identity.ClientID)
	} else {
		c.PrincipalID = types.StringNull()
		c.ClientID = types.StringNull()
	}
}

// optionalString keeps an unset optional attribute null rather than empty, so
// Terraform does not report a permanent diff against an absent API field.
func optionalString(value string) types.String {
	if value == "" {
		return types.StringNull()
	}
	return types.StringValue(value)
}

// createAgent publishes the first version of an agent and returns it.
func createAgent(ctx context.Context, client *clients.Client, name, description string, definition any) (agentVersion, error) {
	var created agentResponse
	err := client.JSON(ctx, http.MethodPost, "agents", agentRequest{
		Name:        name,
		Description: description,
		Definition:  definition,
	}, &created)
	return created.Versions.Latest, err
}

// readAgent returns the latest version of an agent.
func readAgent(ctx context.Context, client *clients.Client, name string) (agentVersion, error) {
	var agent agentResponse
	err := client.JSON(ctx, http.MethodGet, "agents/"+name, nil, &agent)
	return agent.Versions.Latest, err
}

// updateAgent publishes a new version, because the service treats each agent
// version as immutable and PATCH does not change the definition.
func updateAgent(ctx context.Context, client *clients.Client, name, description string, definition any) (agentVersion, error) {
	var version agentVersion
	err := client.JSON(ctx, http.MethodPost, "agents/"+name+"/versions", agentRequest{
		Description: description,
		Definition:  definition,
	}, &version)
	return version, err
}

// deleteAgent removes an agent, treating an already-absent agent as success.
func deleteAgent(ctx context.Context, client *clients.Client, name string) error {
	err := client.JSON(ctx, http.MethodDelete, "agents/"+name, nil, nil)
	if clients.IsNotFound(err) {
		return nil
	}
	return err
}

// clientFromProviderData resolves the configured client shared by every resource and data source.
func clientFromProviderData(providerData any, diagnostics *diag.Diagnostics) *clients.Client {
	if providerData == nil {
		return nil
	}
	client, ok := providerData.(*clients.Client)
	if !ok {
		diagnostics.AddError("Unexpected provider data", fmt.Sprintf("Expected *clients.Client, got %T.", providerData))
		return nil
	}
	return client
}

// decodeDefinition unmarshals an agent definition into a kind-specific struct.
func decodeDefinition(version agentVersion, target any, diagnostics *diag.Diagnostics) bool {
	if err := json.Unmarshal(version.Definition, target); err != nil {
		diagnostics.AddError("Unable to decode agent definition", err.Error())
		return false
	}
	return true
}
