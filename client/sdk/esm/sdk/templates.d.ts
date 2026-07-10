import { ClientSDK, RequestOptions } from "../lib/sdks.js";
import { CreatePromptTemplateResult } from "../models/components/createprompttemplateresult.js";
import { GetPromptTemplateResult } from "../models/components/getprompttemplateresult.js";
import { ListPromptTemplatesResult } from "../models/components/listprompttemplatesresult.js";
import { RenderTemplateResult } from "../models/components/rendertemplateresult.js";
import { UpdatePromptTemplateResult } from "../models/components/updateprompttemplateresult.js";
import { CreateTemplateRequest, CreateTemplateSecurity } from "../models/operations/createtemplate.js";
import { DeleteTemplateRequest, DeleteTemplateSecurity } from "../models/operations/deletetemplate.js";
import { GetTemplateRequest, GetTemplateSecurity } from "../models/operations/gettemplate.js";
import { ListTemplatesRequest, ListTemplatesSecurity } from "../models/operations/listtemplates.js";
import { RenderTemplateRequest, RenderTemplateSecurity } from "../models/operations/rendertemplate.js";
import { RenderTemplateByIDRequest, RenderTemplateByIDSecurity } from "../models/operations/rendertemplatebyid.js";
import { UpdateTemplateRequest, UpdateTemplateSecurity } from "../models/operations/updatetemplate.js";
export declare class Templates extends ClientSDK {
    /**
     * createTemplate templates
     *
     * @remarks
     * Create a new prompt template.
     */
    create(request: CreateTemplateRequest, security?: CreateTemplateSecurity | undefined, options?: RequestOptions): Promise<CreatePromptTemplateResult>;
    /**
     * deleteTemplate templates
     *
     * @remarks
     * Delete prompt template by its ID or name.
     */
    delete(request?: DeleteTemplateRequest | undefined, security?: DeleteTemplateSecurity | undefined, options?: RequestOptions): Promise<void>;
    /**
     * getTemplate templates
     *
     * @remarks
     * Get prompt template by its ID or name.
     */
    get(request?: GetTemplateRequest | undefined, security?: GetTemplateSecurity | undefined, options?: RequestOptions): Promise<GetPromptTemplateResult>;
    /**
     * listTemplates templates
     *
     * @remarks
     * List available prompt template.
     */
    list(request?: ListTemplatesRequest | undefined, security?: ListTemplatesSecurity | undefined, options?: RequestOptions): Promise<ListPromptTemplatesResult>;
    /**
     * renderTemplateByID templates
     *
     * @remarks
     * Render a prompt template by ID with provided input data.
     */
    renderByID(request: RenderTemplateByIDRequest, security?: RenderTemplateByIDSecurity | undefined, options?: RequestOptions): Promise<RenderTemplateResult>;
    /**
     * renderTemplate templates
     *
     * @remarks
     * Render a prompt template directly with all template fields provided.
     */
    render(request: RenderTemplateRequest, security?: RenderTemplateSecurity | undefined, options?: RequestOptions): Promise<RenderTemplateResult>;
    /**
     * updateTemplate templates
     *
     * @remarks
     * Update a prompt template.
     */
    update(request: UpdateTemplateRequest, security?: UpdateTemplateSecurity | undefined, options?: RequestOptions): Promise<UpdatePromptTemplateResult>;
}
//# sourceMappingURL=templates.d.ts.map