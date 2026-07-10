import { ClientSDK, RequestOptions } from "../lib/sdks.js";
import { ListTriggerDefinitionsResult } from "../models/components/listtriggerdefinitionsresult.js";
import { ListTriggerInstancesResult } from "../models/components/listtriggerinstancesresult.js";
import { TriggerInstance } from "../models/components/triggerinstance.js";
import { CreateTriggerInstanceRequest, CreateTriggerInstanceSecurity } from "../models/operations/createtriggerinstance.js";
import { DeleteTriggerInstanceRequest, DeleteTriggerInstanceSecurity } from "../models/operations/deletetriggerinstance.js";
import { GetTriggerInstanceRequest, GetTriggerInstanceSecurity } from "../models/operations/gettriggerinstance.js";
import { ListTriggerDefinitionsRequest, ListTriggerDefinitionsSecurity } from "../models/operations/listtriggerdefinitions.js";
import { ListTriggerInstancesRequest, ListTriggerInstancesSecurity } from "../models/operations/listtriggerinstances.js";
import { PauseTriggerInstanceRequest, PauseTriggerInstanceSecurity } from "../models/operations/pausetriggerinstance.js";
import { ResumeTriggerInstanceRequest, ResumeTriggerInstanceSecurity } from "../models/operations/resumetriggerinstance.js";
import { UpdateTriggerInstanceRequest, UpdateTriggerInstanceSecurity } from "../models/operations/updatetriggerinstance.js";
export declare class Triggers extends ClientSDK {
    /**
     * createTriggerInstance triggers
     *
     * @remarks
     * Create a trigger instance.
     */
    create(request: CreateTriggerInstanceRequest, security?: CreateTriggerInstanceSecurity | undefined, options?: RequestOptions): Promise<TriggerInstance>;
    /**
     * deleteTriggerInstance triggers
     *
     * @remarks
     * Delete a trigger instance.
     */
    delete(request: DeleteTriggerInstanceRequest, security?: DeleteTriggerInstanceSecurity | undefined, options?: RequestOptions): Promise<void>;
    /**
     * getTriggerInstance triggers
     *
     * @remarks
     * Get a trigger instance by ID.
     */
    get(request: GetTriggerInstanceRequest, security?: GetTriggerInstanceSecurity | undefined, options?: RequestOptions): Promise<TriggerInstance>;
    /**
     * listTriggerInstances triggers
     *
     * @remarks
     * List trigger instances for the current project.
     */
    list(request?: ListTriggerInstancesRequest | undefined, security?: ListTriggerInstancesSecurity | undefined, options?: RequestOptions): Promise<ListTriggerInstancesResult>;
    /**
     * listTriggerDefinitions triggers
     *
     * @remarks
     * List static trigger definitions available to a project.
     */
    listDefinitions(request?: ListTriggerDefinitionsRequest | undefined, security?: ListTriggerDefinitionsSecurity | undefined, options?: RequestOptions): Promise<ListTriggerDefinitionsResult>;
    /**
     * pauseTriggerInstance triggers
     *
     * @remarks
     * Pause a trigger instance.
     */
    pause(request: PauseTriggerInstanceRequest, security?: PauseTriggerInstanceSecurity | undefined, options?: RequestOptions): Promise<TriggerInstance>;
    /**
     * resumeTriggerInstance triggers
     *
     * @remarks
     * Resume a trigger instance.
     */
    resume(request: ResumeTriggerInstanceRequest, security?: ResumeTriggerInstanceSecurity | undefined, options?: RequestOptions): Promise<TriggerInstance>;
    /**
     * updateTriggerInstance triggers
     *
     * @remarks
     * Update a trigger instance.
     */
    update(request: UpdateTriggerInstanceRequest, security?: UpdateTriggerInstanceSecurity | undefined, options?: RequestOptions): Promise<TriggerInstance>;
}
//# sourceMappingURL=triggers.d.ts.map