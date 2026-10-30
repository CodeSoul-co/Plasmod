export interface Scope { tenant_id: string; workspace_id: string; agent_id?: string; session_id?: string }
export interface RequestOptions { headers?: Record<string, string>; signal?: AbortSignal }
export interface DynamicEvent {
 schema_version?: string;
 identity: { event_id: string; tenant_id?: string; workspace_id?: string; source?: string };
 actor?: { agent_id?: string; session_id?: string };
 event?: { event_type?: string; importance?: number; confidence?: number };
 access?: { visibility?: string; consistency?: string; visible_to_agents?: string[]; policy_tags?: string[] };
 payload: Record<string, unknown>;
 [key: string]: unknown;
}
export interface Memory extends Scope {
 memory_id: string; content: string; summary: string; memory_type: string; lifecycle_state?: string;
 is_active: boolean; source_event_ids: string[]; version: number; confidence: number;
 source_spans?: { event_id: string; start: number; end: number; quote: string; observed_at: number }[];
 summary_method?: "extractive" | "semantic";
 [key: string]: unknown;
}
export interface MaintenanceRequest extends Scope {
 operation_id: string; operation: "ingest" | "update" | "decay" | "compress" | "summarize";
 memory_ids?: string[]; signals?: Record<string, number>; now_ts?: string; limit?: number; after_id?: string;
}
export interface MaintenanceReceipt {
 operation_id: string; operation: string; chain_name: "memory_pipeline_chain";
 status: "running" | "completed" | "partial" | "empty";
 selected_count: number; updated_count: number; skipped_count: number; failed_count: number;
 produced_ids?: string[]; next_cursor?: string; has_more: boolean;
 items: { memory_id: string; status: "updated" | "skipped" | "failed"; reason: string; before?: string; after?: string; source_event_ids?: string[] }[];
 request: MaintenanceRequest; updated_at: string;
}
export interface MutationRequest extends Pick<Scope, "tenant_id" | "workspace_id"> {
 operation_id: string; operation: "forget" | "correct"; reference_ids: string[]; replacement?: DynamicEvent;
}
export interface MutationReceipt {
 operation_id: string; operation: string; chain_name: "memory_pipeline_chain";
 status: "running" | "partial" | "completed"; forgotten_ids: string[]; replacement_id?: string; error?: string;
}
export interface DocumentRequest extends Scope {
 text: string; title?: string; source_id?: string; idempotency_key?: string; chunk_size?: number; overlap?: number;
 upload_batch_id?: string; segment_index?: number; segment_total?: number; importance?: number;
}
export interface DocumentReceipt {
 status: "ok" | "partial" | "accumulating"; batch_id: string; source_id?: string;
 memory_ids?: string[]; failed_count?: number; chunks?: number; missing_segments?: number[];
 receipts?: {index: number; event_id: string; start: number; end: number; status: string; error?: string}[];
}
export interface QueryRequest extends Partial<Scope> {query_text: string; top_k?: number; response_mode?: string; query_scope?: string; [key: string]: unknown}
export interface QueryResponse {objects: string[]; [key: string]: unknown}
export interface IngestReceipt {status: string; event_id?: string; memory_id?: string; lsn?: number; chain_name?: string; [key:string]:unknown}
export class PlasmodError extends Error {status: number; code: string; detail?: string; effectUnknown: boolean}
export class PlasmodClient {
 constructor(baseUrl?: string, options?: {headers?: Record<string,string>; fetch?: typeof fetch});
 request(path: string, options?: RequestInit): Promise<unknown>;
 getConsistencyMode(): Promise<unknown>;
 setConsistencyMode(mode: string): Promise<unknown>;
 ingestEvent(event: DynamicEvent, options?: RequestOptions): Promise<IngestReceipt>;
 ingestDocument(document: DocumentRequest, options?: RequestOptions): Promise<DocumentReceipt>;
 query(query: QueryRequest, options?: RequestOptions): Promise<QueryResponse>;
 recall(query: Partial<Scope> & {query:string; scope?:string; top_k?:number}, options?: RequestOptions): Promise<unknown>;
 listMemories(scope: Scope, options?: RequestOptions): Promise<Memory[]>;
 listStates(scope: Scope, options?: RequestOptions): Promise<Record<string,unknown>[]>;
 evidence(id: string, scope: Scope, options?: RequestOptions): Promise<Record<string,unknown>>;
 maintain(request: MaintenanceRequest, options?: RequestOptions): Promise<MaintenanceReceipt>;
 maintenanceStatus(id: string, scope: Scope, options?: RequestOptions): Promise<MaintenanceReceipt>;
 mutate(request: MutationRequest, options?: RequestOptions): Promise<MutationReceipt>;
 forget(request: Omit<MutationRequest,"operation">, options?: RequestOptions): Promise<MutationReceipt>;
 correct(request: Omit<MutationRequest,"operation"> & {replacement:DynamicEvent}, options?: RequestOptions): Promise<MutationReceipt>;
 mutationStatus(id: string, scope: Scope, options?: RequestOptions): Promise<MutationReceipt>;
}
export class AndbClient extends PlasmodClient {}
