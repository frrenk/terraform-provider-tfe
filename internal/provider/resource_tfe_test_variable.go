// Copyright (c) HashiCorp, Inc.
// SPDX-License-Identifier: MPL-2.0

package provider

import (
	"context"
	"errors"
	"fmt"
	"log"

	tfe "github.com/hashicorp/go-tfe"
	"github.com/hashicorp/terraform-plugin-framework-validators/stringvalidator"
	"github.com/hashicorp/terraform-plugin-framework/path"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/booldefault"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/boolplanmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/planmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/stringdefault"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/stringplanmodifier"
	"github.com/hashicorp/terraform-plugin-framework/schema/validator"
	"github.com/hashicorp/terraform-plugin-framework/types"
)

type resourceTFETestVariable struct {
	config ConfiguredClient
}

// modelTFETestVariable maps the resource schema data to a struct.
type modelTFETestVariable struct {
	ID             types.String `tfsdk:"id"`
	Key            types.String `tfsdk:"key"`
	Value          types.String `tfsdk:"value"`
	ReadableValue  types.String `tfsdk:"readable_value"`
	Category       types.String `tfsdk:"category"`
	Description    types.String `tfsdk:"description"`
	HCL            types.Bool   `tfsdk:"hcl"`
	Sensitive      types.Bool   `tfsdk:"sensitive"`
	Organization   types.String `tfsdk:"organization"`
	ModuleName     types.String `tfsdk:"module_name"`
	ModuleProvider types.String `tfsdk:"module_provider"`
}

// modelFromTFETestVariable builds a modelTFETestVariable struct from a tfe.TestVariable
// value (plus the last known value of the variable's `value` attribute).
func modelFromTFETestVariable(v tfe.Variable, lastValue types.String) modelTFETestVariable {
	// Initialize all fields from the provided API struct
	m := modelTFETestVariable{
		ID:             types.StringValue(v.ID),
		Key:            types.StringValue(v.Key),
		Value:          types.StringValue(v.Value),
		Category:       types.StringValue(string(v.Category)),
		Description:    types.StringValue(v.Description),
		HCL:            types.BoolValue(v.HCL),
		Sensitive:      types.BoolValue(v.Sensitive),
		Organization:   types.StringUnknown(), // wip
		ModuleName:     types.StringUnknown(), // wip
		ModuleProvider: types.StringUnknown(), // wip
	}
	// BUT: if the variable is sensitive, carry forward the last known value
	// instead, because the API never lets us read it again.
	if v.Sensitive {
		m.Value = lastValue
		m.ReadableValue = types.StringNull()
	} else {
		m.ReadableValue = m.Value
	}
	return m
}

// Configure implements resource.ResourceWithConfigure
func (r *resourceTFETestVariable) Configure(ctx context.Context, req resource.ConfigureRequest, resp *resource.ConfigureResponse) {
	// Early exit if provider is unconfigured (i.e. we're only validating config or something)
	if req.ProviderData == nil {
		return
	}
	client, ok := req.ProviderData.(ConfiguredClient)
	if !ok {
		resp.Diagnostics.AddError(
			"Unexpected resource Configure type",
			fmt.Sprintf("Expected tfe.ConfiguredClient, got %T. This is a bug in the tfe provider, so please report it on GitHub.", req.ProviderData),
		)
	}
	r.config = client
}

// Metadata implements resource.Resource
func (r *resourceTFETestVariable) Metadata(_ context.Context, _ resource.MetadataRequest, resp *resource.MetadataResponse) {
	resp.TypeName = "tfe_test_variable"
}

// Schema implements resource.Resource
func (r *resourceTFETestVariable) Schema(ctx context.Context, req resource.SchemaRequest, resp *resource.SchemaResponse) {
	resp.Schema = schema.Schema{
		Attributes: map[string]schema.Attribute{
			"id": schema.StringAttribute{
				Computed:    true,
				Description: "Service-generated identifier for the variable",
				PlanModifiers: []planmodifier.String{
					stringplanmodifier.UseStateForUnknown(),
				},
			},
			"key": schema.StringAttribute{
				Required:    true,
				Description: "Name of the variable.",
				PlanModifiers: []planmodifier.String{
					stringplanmodifier.RequiresReplaceIf(
						func(ctx context.Context, req planmodifier.StringRequest, resp *stringplanmodifier.RequiresReplaceIfFuncResponse) {
							var stateSensitive types.Bool
							diags := req.State.GetAttribute(ctx, path.Root("sensitive"), &stateSensitive)
							if diags.HasError() {
								resp.Diagnostics.Append(diags...)
								return
							}
							if stateSensitive.ValueBool() && req.PlanValue.ValueString() != req.StateValue.ValueString() {
								resp.RequiresReplace = true
							}
						},
						"Force replacement if key changed and sensitive is true",
						"Force replacement if key changed and sensitive is true",
					),
				},
			},
			"value": schema.StringAttribute{
				Optional:    true,
				Computed:    true,
				Default:     stringdefault.StaticString(""),
				Sensitive:   true,
				Description: "Value of the variable",
			},
			"category": schema.StringAttribute{
				Required:    true,
				Description: `Whether this is a Terraform or environment variable. Valid values are "terraform" or "env".`,
				Validators: []validator.String{
					stringvalidator.OneOf(
						string(tfe.CategoryEnv),
					),
				},
				PlanModifiers: []planmodifier.String{
					stringplanmodifier.RequiresReplace(),
				},
			},
			"description": schema.StringAttribute{
				Optional: true,
				Computed: true,
				Default:  stringdefault.StaticString(""),
			},
			"hcl": schema.BoolAttribute{
				Optional: true,
				Computed: true,
				Default:  booldefault.StaticBool(false),
			},
			"sensitive": schema.BoolAttribute{
				Optional: true,
				Computed: true,
				Default:  booldefault.StaticBool(false),
				PlanModifiers: []planmodifier.Bool{
					boolplanmodifier.RequiresReplaceIf(
						func(ctx context.Context, req planmodifier.BoolRequest, resp *boolplanmodifier.RequiresReplaceIfFuncResponse) {
							if req.StateValue.ValueBool() && !req.ConfigValue.ValueBool() {
								resp.RequiresReplace = true
							}
						},
						"Force replacement if sensitive argument changed from true to false.",
						"Force replacement if sensitive argument changed from true to false.",
					),
				},
			},
			"organization": schema.StringAttribute{
				Optional: true,
			},
			"module_name": schema.StringAttribute{
				Optional: true,
			},
			"module_provider": schema.StringAttribute{
				Optional: true,
			},
			"readable_value": schema.StringAttribute{
				Computed: true,
				Description: "A non-sensitive read-only copy of the variable value, which can be viewed or referenced " +
					"in plan outputs without being redacted. Will only be present if the variable is not sensitive",
				PlanModifiers: []planmodifier.String{
					&updateReadableValuePlanModifier{},
				},
			},
		},
		Description:         "",
		MarkdownDescription: "",
		DeprecationMessage:  "",
		Version:             1,
	}
}

// Create implements resource.Resource
func (r *resourceTFETestVariable) Create(ctx context.Context, req resource.CreateRequest, resp *resource.CreateResponse) {
	r.createWithTestVariable(ctx, req, resp)
}

// createWithTestVariable is the test version of Create.
func (r *resourceTFETestVariable) createWithTestVariable(ctx context.Context, req resource.CreateRequest, resp *resource.CreateResponse) {
	var data modelTFETestVariable
	diags := req.Plan.Get(ctx, &data)
	resp.Diagnostics.Append(diags...)
	if resp.Diagnostics.HasError() {
		return
	}

	key := data.Key.ValueString()
	category := data.Category.ValueString()
	moduleId := tfe.RegistryModuleID{
		Organization: data.Organization.ValueString(),
		Name:         data.ModuleName.ValueString(),
		Provider:     data.ModuleProvider.ValueString(),
		Namespace:    data.Organization.ValueString(),
		RegistryName: "private",
	}

	options := tfe.VariableCreateOptions{
		Key:         data.Key.ValueStringPointer(),
		Value:       data.Value.ValueStringPointer(),
		Category:    tfe.Category(tfe.CategoryType(category)),
		HCL:         data.HCL.ValueBoolPointer(),
		Sensitive:   data.Sensitive.ValueBoolPointer(),
		Description: data.Description.ValueStringPointer(),
	}

	log.Printf("[DEBUG] Create %s variable: %s", category, key)
	variable, err := r.config.Client.TestVariables.Create(ctx, moduleId, options)
	if err != nil {
		resp.Diagnostics.AddError(
			"Error creating variable",
			fmt.Sprintf("Couldn't create %s variable %s: %s", category, key, err.Error()),
		)
		return
	}

	// We got a variable, so set state to new values
	result := modelFromTFETestVariable(*variable, data.Value)
	diags = resp.State.Set(ctx, &result)
	resp.Diagnostics.Append(diags...)
}

// Update implements resource.Resource
func (r *resourceTFETestVariable) Update(ctx context.Context, req resource.UpdateRequest, resp *resource.UpdateResponse) {
	r.updateWithTestConfig(ctx, req, resp)
}

// updateWithTestConfig is the test config version of Update.
func (r *resourceTFETestVariable) updateWithTestConfig(ctx context.Context, req resource.UpdateRequest, resp *resource.UpdateResponse) {
	var plan modelTFETestVariable
	var state modelTFETestVariable
	diags := req.Plan.Get(ctx, &plan)
	resp.Diagnostics.Append(diags...)
	if resp.Diagnostics.HasError() {
		return
	}
	diags = req.State.Get(ctx, &state)
	resp.Diagnostics.Append(diags...)
	if resp.Diagnostics.HasError() {
		return
	}

	variableID := plan.ID.ValueString()
	moduleId := tfe.RegistryModuleID{
		Organization: plan.Organization.ValueString(),
		Name:         plan.ModuleName.ValueString(),
		Provider:     plan.ModuleProvider.ValueString(),
		Namespace:    plan.Organization.ValueString(),
		RegistryName: "private",
	}

	options := tfe.VariableUpdateOptions{
		Key:         plan.Key.ValueStringPointer(),
		Description: plan.Description.ValueStringPointer(),
		HCL:         plan.HCL.ValueBoolPointer(),
		Sensitive:   plan.Sensitive.ValueBoolPointer(),
	}
	// We ONLY want to set Value if our planned value would be a CHANGE from the
	// prior state. See comments in updateWithWorkspace for more color.
	if state.Value.ValueString() != plan.Value.ValueString() {
		options.Value = plan.Value.ValueStringPointer()
	}

	log.Printf("[DEBUG] Update variable: %s", variableID)
	variable, err := r.config.Client.TestVariables.Update(ctx, moduleId, variableID, options)
	if err != nil {
		resp.Diagnostics.AddError(
			"Error updating variable",
			fmt.Sprintf("Couldn't update variable %s: %s", variableID, err.Error()),
		)
		return
	}
	// Update state
	result := modelFromTFETestVariable(*variable, plan.Value)
	diags = resp.State.Set(ctx, &result)
	resp.Diagnostics.Append(diags...)
}

// Delete implements resource.Resource
func (r *resourceTFETestVariable) Delete(ctx context.Context, req resource.DeleteRequest, resp *resource.DeleteResponse) {
	r.deleteWithTestConfig(ctx, req, resp)
}

// deleteWithTestConfig is the test config version of Delete.
func (r *resourceTFETestVariable) deleteWithTestConfig(ctx context.Context, req resource.DeleteRequest, resp *resource.DeleteResponse) {
	var data modelTFETestVariable
	diags := req.State.Get(ctx, &data)
	resp.Diagnostics.Append(diags...)
	if resp.Diagnostics.HasError() {
		return
	}

	variableID := data.ID.ValueString()
	moduleId := tfe.RegistryModuleID{
		Organization: data.Organization.ValueString(),
		Name:         data.ModuleName.ValueString(),
		Provider:     data.ModuleProvider.ValueString(),
		Namespace:    data.Organization.ValueString(),
		RegistryName: "private",
	}
	log.Printf("[DEBUG] Delete variable: %s", variableID)
	err := r.config.Client.TestVariables.Delete(ctx, moduleId, variableID)
	// Ignore 404s for delete
	if err != nil && !errors.Is(err, tfe.ErrResourceNotFound) {
		resp.Diagnostics.AddError(
			"Error deleting variable",
			fmt.Sprintf("Couldn't delete variable %s: %s", variableID, err.Error()),
		)
	}
	// Resource is implicitly deleted from resp.State if diagnostics have no errors.
}