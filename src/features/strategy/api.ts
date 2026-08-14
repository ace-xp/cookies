import { BackendApiError, apiRequest } from '../../backend/platform'
import type {
  AgentTask,
  AgentTaskInspection,
  ApplyArtifactProposalResult,
  ArtifactProposal,
  BriefCenterDetail,
  BriefCenterSummary,
  BriefDraft,
	BriefPatchOperation,
  BriefVersion,
  ConversationBundle,
  ConversationCapabilities,
  ConversationMemory,
  CreativeBusinessProfile,
  CreativeBusinessCapability,
  CreativeBusinessRecommendationSnapshot,
  CreativeTaskPlan,
  CreativeIntakeV3,
  CreativeIntakeV4,
  DeepReviewAnalysis,
  DraftRevision,
  EvidenceReference,
  GenerationMetadata,
  GenerationProbe,
  GenerationReadiness,
	KnowledgeDocument,
	DocumentVisionFallbackCapability,
	DocumentPreview,
  KnowledgeJobControl,
  MediaUnderstandingArtifact,
  Message,
  MessageCreateV2,
  PackageVersion,
  ProjectContextManifest,
  StrategyCreativeHandoff,
  ResearchRun,
  ResearchArtifact,
  Review,
  ReviewComment,
  ReviewPolicy,
  SkillRun,
  SkillDescriptor,
  StrategyDraft,
  StrategyP0Metrics,
  StrategyCenterSummary,
  StrategyTask,
  StrategyTaskBundle,
  StrategyTaskListItem,
  TaskActivitySnapshot,
  Workspace,
  WorkspaceDetail,
} from './types'

const root = '/api/strategy/v1'

export function createMutationKey(prefix = 'strategy-kanon') {
  const id = globalThis.crypto?.randomUUID?.() ?? `${Date.now()}-${Math.random().toString(16).slice(2)}`
  return `${prefix}-${id}`
}

function mutationHeaders(key?: string) {
  return { 'Idempotency-Key': key ?? createMutationKey() }
}

export const strategyApi = {
  listSkills: (signal?: AbortSignal) =>
    apiRequest<{ items: SkillDescriptor[] }>(`${root}/skills`, { signal }),

  listWorkspaces: (projectId: string, signal?: AbortSignal) =>
    apiRequest<{ items: Workspace[] }>(`${root}/projects/${encodeURIComponent(projectId)}/workspaces`, { signal }),

  listActivities: (projectId: string, workspaceId = '', signal?: AbortSignal) => {
    const query = new URLSearchParams({ limit: '50' })
    if (workspaceId) query.set('workspace_id', workspaceId)
    return apiRequest<TaskActivitySnapshot>(
      `${root}/projects/${encodeURIComponent(projectId)}/activities?${query.toString()}`,
      { signal },
    )
  },

  getP0Metrics: (projectId: string, days = 30, signal?: AbortSignal) =>
    apiRequest<StrategyP0Metrics>(
      `${root}/projects/${encodeURIComponent(projectId)}/p0-metrics?days=${days}`,
      { signal },
    ),

  listTasks: (projectId: string, lifecycle: 'active' | 'archived' | 'all' = 'active', signal?: AbortSignal) =>
    apiRequest<{ items: StrategyTaskListItem[] }>(
      `${root}/projects/${encodeURIComponent(projectId)}/tasks?lifecycle=${lifecycle}`,
      { signal },
    ),

  listProjectBriefs: (projectId: string, signal?: AbortSignal) =>
    apiRequest<{ items: BriefCenterSummary[] }>(
      `${root}/projects/${encodeURIComponent(projectId)}/briefs`,
      { signal },
    ),

  getProjectBrief: (projectId: string, briefId: string, signal?: AbortSignal) =>
    apiRequest<BriefCenterDetail>(
      `${root}/projects/${encodeURIComponent(projectId)}/briefs/${encodeURIComponent(briefId)}`,
      { signal },
    ),

  listProjectStrategies: (projectId: string, signal?: AbortSignal) =>
    apiRequest<{ items: StrategyCenterSummary[] }>(
      `${root}/projects/${encodeURIComponent(projectId)}/strategy-drafts`,
      { signal },
    ),

  listCreativeBusinesses: (projectId: string, signal?: AbortSignal) =>
    apiRequest<{ catalog_hash: string; items: CreativeBusinessProfile[] }>(
      `${root}/projects/${encodeURIComponent(projectId)}/creative-businesses`,
      { signal },
    ),

  recommendCreativeBusinesses: (
    projectId: string,
    brief: BriefVersion,
    signal?: AbortSignal,
  ) =>
    apiRequest<CreativeBusinessRecommendationSnapshot>(
      `${root}/projects/${encodeURIComponent(projectId)}/creative-business-recommendations`,
      {
        method: 'POST',
        body: JSON.stringify({
          brief_id: brief.brief_id,
          brief_version: brief.version,
          limit: 3,
        }),
        signal,
      },
    ),

  listCreativeTaskPlans: (projectId: string, briefId = '', signal?: AbortSignal) =>
    apiRequest<{ items: CreativeTaskPlan[] }>(
      `${root}/projects/${encodeURIComponent(projectId)}/creative-task-plans${briefId ? `?brief_id=${encodeURIComponent(briefId)}` : ''}`,
      { signal },
    ),

  createCreativeTaskPlan: (
    projectId: string,
    input: {
      contract_version: 'strategy-creative-task-plan/v2'
      strategy_package_ref: {
        package_id: string
        package_version: number
        package_content_hash: string
        handoff_contract_version: 'strategy-creative-handoff/v1'
        handoff_content_hash: string
      }
      selected_route_id: string
      business_code: string
      selection_source: 'recommended' | 'manual'
      catalog_hash: string
    },
    mutationKey?: string,
  ) =>
    apiRequest<CreativeTaskPlan>(
      `${root}/projects/${encodeURIComponent(projectId)}/creative-task-plans`,
      {
        method: 'POST',
        headers: mutationHeaders(mutationKey),
        body: JSON.stringify(input),
      },
    ),

  getStrategyCreativeHandoff: (
    projectId: string,
    packageId: string,
    packageVersion: number,
    signal?: AbortSignal,
  ) =>
    apiRequest<StrategyCreativeHandoff>(
      `${root}/projects/${encodeURIComponent(projectId)}/strategy-packages/${encodeURIComponent(packageId)}/versions/${packageVersion}/creative-handoff`,
      { signal },
    ),

  patchCreativeTaskPlanAnswers: (
    plan: CreativeTaskPlan,
    answers: Record<string, unknown>,
    mutationKey?: string,
  ) =>
    apiRequest<CreativeTaskPlan>(
      `${root}/creative-task-plans/${encodeURIComponent(plan.id)}/answers`,
      {
        method: 'PATCH',
        headers: { ...mutationHeaders(mutationKey), 'If-Match': `"v${plan.version}"` },
        body: JSON.stringify({
          expected_version: plan.version,
          operations: Object.entries(answers).map(([question_id, value]) => ({
            op: value === undefined ? 'remove' : 'set',
            question_id,
            ...(value === undefined ? {} : { value }),
          })),
        }),
      },
    ),

  generateCreativeTaskStrategy: (plan: CreativeTaskPlan, mutationKey?: string) =>
    apiRequest<{ plan: CreativeTaskPlan; agent_task: AgentTask }>(
      `${root}/creative-task-plans/${encodeURIComponent(plan.id)}:generate`,
      {
        method: 'POST',
        headers: { ...mutationHeaders(mutationKey), 'If-Match': `"v${plan.version}"` },
        body: JSON.stringify({
          expected_version: plan.version,
          expected_revision: plan.current_revision,
        }),
      },
    ),

  getCreativeTaskPlan: (planId: string, signal?: AbortSignal) =>
    apiRequest<CreativeTaskPlan>(
      `${root}/creative-task-plans/${encodeURIComponent(planId)}`,
      { signal },
    ),

  listCreativeBusinessCapabilities: (projectId: string, signal?: AbortSignal) =>
    apiRequest<{ items: CreativeBusinessCapability[] }>(
      `/api/creative/v1/projects/${encodeURIComponent(projectId)}/business-capabilities`,
      { signal },
    ),

  handoffCreativeTaskStrategy: (
    projectId: string,
    plan: CreativeTaskPlan,
    mutationKey?: string,
  ) => {
    if (!plan.package_ref || !plan.selected_route_id) {
      throw new Error('任务规格缺少策略包、Handoff 或 Route 血缘')
    }
    return apiRequest<CreativeIntakeV3>(
      `/api/creative/v1/projects/${encodeURIComponent(projectId)}/creative-intakes`,
      {
        method: 'POST',
        headers: mutationHeaders(mutationKey),
        body: JSON.stringify({
          contract_version: 'creative-intake-create/v3',
          source: 'strategy_package',
          strategy_package_ref: plan.package_ref,
          selected_route_id: plan.selected_route_id,
          ...(plan.current_strategy?.task_overlay_ref
            ? { task_overlay_ref: plan.current_strategy.task_overlay_ref }
            : {}),
        }),
      },
    )
  },

  prepareStrategyBrandWorkflow: (
    projectId: string,
    intake: CreativeIntakeV3,
  ) => apiRequest<{ mode: string; next_action: string }>(
    `/api/creative/v1/projects/${encodeURIComponent(projectId)}/creative-intakes/${encodeURIComponent(intake.id)}/brand-workflow:prepare`,
    {
      method: 'POST',
      headers: { 'Idempotency-Key': `strategy-brand-prepare-${intake.input_identity_hash}` },
      body: JSON.stringify({
        expected_input_identity_hash: intake.input_identity_hash,
        selected_route_id: intake.selected_route_id,
        accept_strategy_projection: true,
      }),
    },
  ),

  createRequirementViralIntake: (
	projectId: string,
	brief: BriefVersion,
	capability: CreativeBusinessProfile,
	mutationKey?: string,
  ) =>
	apiRequest<CreativeIntakeV4>(
	  `/api/creative/v1/projects/${encodeURIComponent(projectId)}/creative-intakes`,
	  {
		method: 'POST',
		headers: mutationHeaders(mutationKey),
		body: JSON.stringify({
		  contract_version: 'creative-intake-create/v4',
		  source: 'requirement_snapshot',
		  requirement_snapshot_ref: {
			brief_id: brief.brief_id,
			brief_version: brief.version,
			content_hash: brief.content_hash,
		  },
		  business_capability_ref: {
			business_code: capability.business_code,
			version: capability.version,
			content_hash: capability.content_hash,
		  },
		  selected_route_id: 'route_manual_viral_remake_v1',
		}),
	  },
	),

  createRequirementViralTask: (projectId: string, intake: CreativeIntakeV4) => {
	const sourceVideo = intake.request.manual_viral_remake?.reference_video
	if (!sourceVideo) throw new Error('爆款裂变仍缺少可用的参考视频')
	return apiRequest<{ id: string }>(
	  `/api/creative/v1/projects/${encodeURIComponent(projectId)}/creative-intakes/${encodeURIComponent(intake.id)}:create-video-task`,
	  {
		method: 'POST',
		body: JSON.stringify({
		  selected_route_id: 'route_manual_viral_remake_v1',
		  channel: 'douyin',
		  source_video: sourceVideo,
		  concept: `基于参考结构的${intake.request.manual_viral_remake?.product_name || '原创短视频'}`,
		  prompt: '等待参考视频多模态分析后生成原创改写指令',
		  call_to_action: '了解更多',
		  mandatory_elements: intake.request.mandatory_elements ?? [],
		  prohibited_claims: intake.request.prohibited_claims ?? [],
		  confirm_route: true,
		}),
	  },
	)
  },

  createImageTextTaskFromHandoff: (
    projectId: string,
    intakeId: string,
    focus: string,
  ) =>
    apiRequest<{ id: string }>(
      `/api/creative/v1/projects/${encodeURIComponent(projectId)}/creative-intakes/${encodeURIComponent(intakeId)}:create-task`,
      {
        method: 'POST',
        body: JSON.stringify({ content_type: 'custom', focus }),
      },
    ),

  listEvidenceReferences: (projectId: string, evidenceId = '', signal?: AbortSignal) => {
    const query = evidenceId ? `?evidence_id=${encodeURIComponent(evidenceId)}` : ''
    return apiRequest<{ items: EvidenceReference[] }>(
      `${root}/projects/${encodeURIComponent(projectId)}/evidence-references${query}`,
      { signal },
    )
  },

  createTask: (projectId: string, name: string, objective: string, mutationKey?: string) =>
    apiRequest<StrategyTaskBundle>(`${root}/projects/${encodeURIComponent(projectId)}/tasks`, {
      method: 'POST',
      headers: mutationHeaders(mutationKey),
      body: JSON.stringify({ name, objective }),
    }),

  discardTask: (taskId: string, expectedVersion: number, reason: string, mutationKey?: string) =>
    apiRequest<StrategyTask>(`${root}/tasks/${encodeURIComponent(taskId)}:discard`, {
      method: 'POST',
      headers: mutationHeaders(mutationKey),
      body: JSON.stringify({ expected_version: expectedVersion, reason }),
    }),

  restoreTask: (taskId: string, expectedVersion: number, mutationKey?: string) =>
    apiRequest<StrategyTask>(`${root}/tasks/${encodeURIComponent(taskId)}:restore`, {
      method: 'POST',
      headers: mutationHeaders(mutationKey),
      body: JSON.stringify({ expected_version: expectedVersion }),
    }),

  createWorkspace: (projectId: string, name: string, mutationKey?: string) =>
    apiRequest<Workspace>(`${root}/workspaces`, {
      method: 'POST',
      headers: mutationHeaders(mutationKey),
      body: JSON.stringify({ project_id: projectId, name }),
    }),

  getWorkspace: (workspaceId: string, signal?: AbortSignal) =>
    apiRequest<WorkspaceDetail>(`${root}/workspaces/${encodeURIComponent(workspaceId)}`, { signal }),

  getWorkspaceContextManifest: (workspaceId: string, stage: ProjectContextManifest['stage'], signal?: AbortSignal) =>
    apiRequest<ProjectContextManifest>(
      `${root}/workspaces/${encodeURIComponent(workspaceId)}/context-manifest?stage=${encodeURIComponent(stage)}`,
      { signal },
    ),

  listAssistantProposals: (workspaceId: string, status = 'proposed', signal?: AbortSignal) =>
    apiRequest<{ items: ArtifactProposal[] }>(
      `${root}/workspaces/${encodeURIComponent(workspaceId)}/assistant-proposals?status=${encodeURIComponent(status)}`,
      { signal },
    ),

  applyAssistantProposal: (
    proposal: ArtifactProposal,
    operations = proposal.operations,
    mutationKey?: string,
  ) => apiRequest<ApplyArtifactProposalResult>(
    `${root}/assistant-proposals/${encodeURIComponent(proposal.id)}:apply`,
    {
      method: 'POST',
      headers: mutationHeaders(mutationKey),
      body: JSON.stringify({
        expected_version: proposal.version,
        ...(operations === proposal.operations ? {} : { operations }),
      }),
    },
  ),

  ignoreAssistantProposal: (proposal: ArtifactProposal, mutationKey?: string) =>
    apiRequest<ArtifactProposal>(`${root}/assistant-proposals/${encodeURIComponent(proposal.id)}:ignore`, {
      method: 'POST',
      headers: mutationHeaders(mutationKey),
      body: JSON.stringify({ expected_version: proposal.version }),
    }),

  probeGeneration: (projectId: string, profile?: 'deep_review') =>
    apiRequest<GenerationProbe>(`${root}/projects/${encodeURIComponent(projectId)}/generation-probe${profile ? `?profile=${profile}` : ''}`, {
      method: 'POST',
    }),

  getDeepReview: (reviewId: string, signal?: AbortSignal) =>
    apiRequest<DeepReviewAnalysis | null>(`${root}/strategy-reviews/${encodeURIComponent(reviewId)}/deep-analysis?optional=1`, { signal }),

  startDeepReview: (reviewId: string, expectedReviewStatus: string, mutationKey?: string) =>
    apiRequest<{ analysis: DeepReviewAnalysis; agent_task: AgentTask }>(
      `${root}/strategy-reviews/${encodeURIComponent(reviewId)}/deep-analysis`,
      {
        method: 'POST',
        headers: mutationHeaders(mutationKey),
        body: JSON.stringify({ expected_review_status: expectedReviewStatus }),
      },
    ),

  getStrategyPerspective: (strategyId: string, signal?: AbortSignal) =>
    apiRequest<DeepReviewAnalysis | null>(`${root}/strategy-drafts/${encodeURIComponent(strategyId)}/perspective-analysis?optional=1`, { signal }),

  startStrategyPerspective: (draft: StrategyDraft, mutationKey?: string) => {
    if (!draft.revision) throw new Error('策略没有可分析的 Revision。')
    return apiRequest<{ analysis: DeepReviewAnalysis; agent_task: AgentTask }>(
      `${root}/strategy-drafts/${encodeURIComponent(draft.id)}/perspective-analysis`,
      {
        method: 'POST',
        headers: mutationHeaders(mutationKey),
        body: JSON.stringify({
          expected_revision: draft.current_revision,
          expected_content_hash: draft.revision.content_hash,
        }),
      },
    )
  },

  createConversation: (projectId: string, workspaceId: string, mutationKey?: string) =>
    apiRequest<ConversationBundle>(`${root}/conversations`, {
      method: 'POST',
      headers: mutationHeaders(mutationKey),
      body: JSON.stringify({ project_id: projectId, workspace_id: workspaceId }),
    }),

  listMessages: (conversationId: string, signal?: AbortSignal) =>
    apiRequest<{ items: Message[] }>(`${root}/conversations/${encodeURIComponent(conversationId)}/messages?limit=100`, { signal }),

  getConversationMemory: (conversationId: string, signal?: AbortSignal) =>
    apiRequest<ConversationMemory>(`${root}/conversations/${encodeURIComponent(conversationId)}/memory`, { signal }),

  compactConversationMemory: (conversationId: string) =>
    apiRequest<ConversationMemory>(`${root}/conversations/${encodeURIComponent(conversationId)}/memory:compact`, {
      method: 'POST',
    }),

  getConversationCapabilities: (signal?: AbortSignal) =>
    apiRequest<ConversationCapabilities>(`${root}/conversation-capabilities`, { signal }),

  sendMessage: (
    conversationId: string,
    content: string | MessageCreateV2,
    mutationKey?: string,
    contextStage: ProjectContextManifest['stage'] = 'intake',
    contextSurface: 'workspace' | 'assistant' = 'workspace',
    excludedSourceIds: string[] = [],
  ) =>
    apiRequest<{ message: Message; agent_task: AgentTask }>(
      `${root}/conversations/${encodeURIComponent(conversationId)}/messages`,
      {
        method: 'POST',
        headers: {
          ...mutationHeaders(mutationKey),
          'X-Strategy-Stage': contextStage,
          'X-Strategy-Surface': contextSurface,
          ...(excludedSourceIds.length
            ? { 'X-Strategy-Excluded-Source-Ids': JSON.stringify(excludedSourceIds) }
            : {}),
        },
        body: JSON.stringify(typeof content === 'string'
          ? contextSurface === 'assistant'
            ? {
                contract_version: 'strategy-conversation-message-create/v2',
                content: [{ type: 'text', text: content }],
              }
            : { content }
          : content),
      },
    ),

  getAgentTask: (agentTaskId: string, signal?: AbortSignal) =>
    apiRequest<AgentTaskInspection>(`${root}/agent-tasks/${encodeURIComponent(agentTaskId)}`, { signal }),

  cancelAgentTask: (agentTaskId: string, expectedVersion: number) =>
    apiRequest<AgentTask>(`${root}/agent-tasks/${encodeURIComponent(agentTaskId)}:cancel`, {
      method: 'POST',
      body: JSON.stringify({ expected_version: expectedVersion }),
    }),

  listSkillRuns: (agentTaskId: string, signal?: AbortSignal) =>
    apiRequest<{ items: SkillRun[] }>(`${root}/agent-tasks/${encodeURIComponent(agentTaskId)}/skill-runs`, { signal }),

  getBriefDraft: (taskId: string, signal?: AbortSignal) =>
    apiRequest<BriefDraft>(`${root}/tasks/${encodeURIComponent(taskId)}/brief-draft`, { signal }),

  patchBriefFields: (
    taskId: string,
    draft: BriefDraft,
    operations: Array<{ fieldPath: string; value: unknown }>,
    mutationKey?: string,
    confirmationMode: 'draft' | 'confirm' = 'confirm',
  ) =>
    apiRequest<BriefDraft>(`${root}/tasks/${encodeURIComponent(taskId)}/brief-draft`, {
      method: 'PATCH',
      headers: { ...mutationHeaders(mutationKey), 'If-Match': `"v${draft.version}"` },
      body: JSON.stringify({
        expected_version: draft.version,
        confirmation_mode: confirmationMode,
        operations: operations.map(operation => ({
          op: 'set',
          field_path: operation.fieldPath,
          value: operation.value,
        })),
      }),
    }),

  patchBriefField: (
    taskId: string,
    draft: BriefDraft,
    fieldPath: string,
    value: unknown,
    mutationKey?: string,
    confirmationMode: 'draft' | 'confirm' = 'confirm',
  ) => strategyApi.patchBriefFields(taskId, draft, [{ fieldPath, value }], mutationKey, confirmationMode),

  confirmBrief: (taskId: string, expectedVersion: number, mutationKey?: string) =>
    apiRequest<BriefVersion>(`${root}/tasks/${encodeURIComponent(taskId)}/brief:confirm`, {
      method: 'POST',
      headers: mutationHeaders(mutationKey),
      body: JSON.stringify({ expected_version: expectedVersion }),
    }),

  createBriefRevisionDraft: (taskId: string, baseBriefVersion: number, mutationKey?: string) =>
    apiRequest<BriefDraft>(`${root}/tasks/${encodeURIComponent(taskId)}/brief-draft:revise`, {
      method: 'POST',
      headers: mutationHeaders(mutationKey),
      body: JSON.stringify({ base_brief_version: baseBriefVersion }),
    }),

  listBriefVersions: (briefId: string, signal?: AbortSignal) =>
    apiRequest<{ items: BriefVersion[] }>(`${root}/briefs/${encodeURIComponent(briefId)}/versions`, { signal }),

  createStrategy: (taskId: string, brief: BriefVersion, mutationKey?: string) =>
    apiRequest<{ strategy_draft: StrategyDraft; agent_task: AgentTask }>(
      `${root}/tasks/${encodeURIComponent(taskId)}/strategies`,
      {
        method: 'POST',
        headers: mutationHeaders(mutationKey),
        body: JSON.stringify({ brief_id: brief.brief_id, brief_version: brief.version }),
      },
    ),

  retryStrategy: (draft: StrategyDraft, mutationKey?: string) =>
    apiRequest<{ strategy_draft: StrategyDraft; agent_task: AgentTask }>(
      `${root}/strategy-drafts/${encodeURIComponent(draft.id)}:retry`,
      {
        method: 'POST',
        headers: mutationHeaders(mutationKey),
        body: JSON.stringify({ expected_version: draft.version }),
      },
    ),

  getGenerationReadiness: (projectId: string, signal?: AbortSignal) =>
    apiRequest<GenerationReadiness>(`${root}/projects/${encodeURIComponent(projectId)}/generation-readiness`, { signal }),

  getStrategy: (strategyId: string, signal?: AbortSignal) =>
    apiRequest<StrategyDraft>(`${root}/strategy-drafts/${encodeURIComponent(strategyId)}`, { signal }),

  archiveStrategy: (strategyId: string, expectedVersion: number, reason: string, mutationKey?: string) =>
    apiRequest<StrategyDraft>(`${root}/strategy-drafts/${encodeURIComponent(strategyId)}:archive`, {
      method: 'POST',
      headers: mutationHeaders(mutationKey),
      body: JSON.stringify({ expected_version: expectedVersion, reason }),
    }),

  restoreStrategy: (strategyId: string, expectedVersion: number, mutationKey?: string) =>
    apiRequest<StrategyDraft>(`${root}/strategy-drafts/${encodeURIComponent(strategyId)}:restore`, {
      method: 'POST',
      headers: mutationHeaders(mutationKey),
      body: JSON.stringify({ expected_version: expectedVersion }),
    }),

  listStrategyRevisions: (strategyId: string, signal?: AbortSignal) =>
    apiRequest<{ items: DraftRevision[] }>(`${root}/strategy-drafts/${encodeURIComponent(strategyId)}/revisions`, { signal }),

  getGenerationMetadata: async (strategyId: string, signal?: AbortSignal) => {
    try {
      return await apiRequest<GenerationMetadata>(
        `${root}/strategy-drafts/${encodeURIComponent(strategyId)}/generation-metadata`,
        { signal },
      )
    } catch (error) {
      if (error instanceof BackendApiError && error.status === 404) return null
      throw error
    }
  },

  patchStrategySection: (draft: StrategyDraft, section: string, value: unknown, mutationKey?: string) =>
    apiRequest<StrategyDraft>(`${root}/strategy-drafts/${encodeURIComponent(draft.id)}`, {
      method: 'PATCH',
      headers: mutationHeaders(mutationKey),
      body: JSON.stringify({
        expected_version: draft.version,
        base_revision: draft.current_revision,
        section,
        value,
      }),
    }),

  reviseStrategy: (draft: StrategyDraft, instruction: string, mutationKey?: string) =>
    apiRequest<AgentTask>(`${root}/strategy-drafts/${encodeURIComponent(draft.id)}:revise`, {
      method: 'POST',
      headers: mutationHeaders(mutationKey),
      body: JSON.stringify({
        expected_version: draft.version,
        base_revision: draft.current_revision,
        instruction,
      }),
    }),

  submitStrategy: (draft: StrategyDraft, mutationKey?: string) =>
    apiRequest<Review>(`${root}/strategy-drafts/${encodeURIComponent(draft.id)}:submit`, {
      method: 'POST',
      headers: mutationHeaders(mutationKey),
      body: JSON.stringify({
        expected_version: draft.version,
        candidate_revision: draft.current_revision,
      }),
    }),

  confirmStrategy: (draft: StrategyDraft, mutationKey?: string) =>
    apiRequest<PackageVersion>(`${root}/strategy-drafts/${encodeURIComponent(draft.id)}:confirm`, {
      method: 'POST',
      headers: mutationHeaders(mutationKey),
      body: JSON.stringify({
        expected_version: draft.version,
        candidate_revision: draft.current_revision,
      }),
    }),

  getReview: (reviewId: string, signal?: AbortSignal) =>
    apiRequest<Review>(`${root}/strategy-reviews/${encodeURIComponent(reviewId)}`, { signal }),

  listReviews: (
    projectId: string,
    filter: 'all' | 'assigned_to_me' | 'requested_by_me' = 'all',
    status = '',
    signal?: AbortSignal,
  ) => {
    const query = new URLSearchParams({ filter })
    if (status) query.set('status', status)
    return apiRequest<{ items: Review[] }>(
      `${root}/projects/${encodeURIComponent(projectId)}/reviews?${query.toString()}`,
      { signal },
    )
  },

  getReviewPolicy: (projectId: string, signal?: AbortSignal) =>
    apiRequest<ReviewPolicy>(
      `${root}/projects/${encodeURIComponent(projectId)}/review-policy`,
      { signal },
    ),

  updateReviewPolicy: (
    projectId: string,
    policy: Pick<ReviewPolicy, 'mode' | 'approver_user_ids' | 'allow_self_approval' | 'version'>,
  ) =>
    apiRequest<ReviewPolicy>(`${root}/projects/${encodeURIComponent(projectId)}/review-policy`, {
      method: 'PUT',
      body: JSON.stringify({
        mode: policy.mode,
        approver_user_ids: policy.approver_user_ids,
        allow_self_approval: policy.allow_self_approval,
        expected_version: policy.version,
      }),
    }),

  listReviewComments: (reviewId: string, signal?: AbortSignal) =>
    apiRequest<{ items: ReviewComment[] }>(
      `${root}/strategy-reviews/${encodeURIComponent(reviewId)}/comments`,
      { signal },
    ),

  addReviewComment: (reviewId: string, body: string) =>
    apiRequest<ReviewComment>(`${root}/strategy-reviews/${encodeURIComponent(reviewId)}/comments`, {
      method: 'POST',
      body: JSON.stringify({ body }),
    }),

  returnReview: (reviewId: string, reason: string) =>
    apiRequest<Review>(`${root}/strategy-reviews/${encodeURIComponent(reviewId)}:return`, {
      method: 'POST',
      body: JSON.stringify({ reason }),
    }),

  approveStrategy: (draft: StrategyDraft, review: Review, mutationKey?: string) =>
    apiRequest<PackageVersion>(`${root}/strategy-drafts/${encodeURIComponent(draft.id)}:approve`, {
      method: 'POST',
      headers: mutationHeaders(mutationKey),
      body: JSON.stringify({
        review_id: review.id,
        candidate_content_hash: review.candidate_content_hash,
        expected_version: draft.version,
      }),
    }),

  listStrategyPackages: (projectId: string, signal?: AbortSignal) =>
    apiRequest<{ items: PackageVersion[] }>(
      `${root}/projects/${encodeURIComponent(projectId)}/strategy-packages`,
      { signal },
    ),

  listKnowledgeDocuments: (projectId: string, signal?: AbortSignal) =>
    apiRequest<{ items: KnowledgeDocument[] }>(
      `/platform/v1/projects/${encodeURIComponent(projectId)}/knowledge/documents`,
      { signal },
    ),

  importKnowledgeDocument: (
    projectId: string,
    input: { title: string; source_uri?: string; source_type: string; text: string },
  ) =>
    apiRequest<KnowledgeDocument>(
      `/platform/v1/projects/${encodeURIComponent(projectId)}/knowledge/documents`,
      { method: 'POST', body: JSON.stringify(input) },
    ),

  uploadKnowledgeDocument: (projectId: string, file: File) => {
    const body = new FormData()
    body.append('file', file)
    return apiRequest<KnowledgeDocument>(
      `/platform/v1/projects/${encodeURIComponent(projectId)}/knowledge/documents`,
      { method: 'POST', body },
    )
  },

  cancelDocumentParse: (projectId: string, documentId: string) =>
    apiRequest<KnowledgeJobControl>(
      `/platform/v1/projects/${encodeURIComponent(projectId)}/knowledge/documents/${encodeURIComponent(documentId)}/cancel`,
      { method: 'POST' },
    ),

  retryDocumentParse: (projectId: string, documentId: string) =>
    apiRequest<KnowledgeDocument>(
      `/platform/v1/projects/${encodeURIComponent(projectId)}/knowledge/documents/${encodeURIComponent(documentId)}/retry`,
      { method: 'POST' },
    ),

  getDocumentVisionFallbackCapability: (projectId: string, documentId: string, signal?: AbortSignal) =>
	apiRequest<DocumentVisionFallbackCapability>(
		`/platform/v1/projects/${encodeURIComponent(projectId)}/knowledge/documents/${encodeURIComponent(documentId)}/visual-fallback-capability`,
		{ signal },
	),

  runDocumentVisionFallback: (projectId: string, documentId: string, pageNumbers: number[] = []) =>
	apiRequest<KnowledgeDocument>(
		`/platform/v1/projects/${encodeURIComponent(projectId)}/knowledge/documents/${encodeURIComponent(documentId)}/run-visual-fallback`,
		{ method: 'POST', body: JSON.stringify({ page_numbers: pageNumbers }) },
	),

  getDocumentPreview: (projectId: string, documentId: string, signal?: AbortSignal) =>
	apiRequest<DocumentPreview>(
		`/platform/v1/projects/${encodeURIComponent(projectId)}/knowledge/documents/${encodeURIComponent(documentId)}/preview`,
		{ signal },
	),

  documentContentUrl: (projectId: string, documentId: string) =>
	`/platform/v1/projects/${encodeURIComponent(projectId)}/knowledge/documents/${encodeURIComponent(documentId)}/content`,

  uploadMediaForUnderstanding: async (projectId: string, file: File) => {
    if (!['image/jpeg', 'image/png', 'video/mp4'].includes(file.type)) {
      throw new Error('仅支持 JPG、PNG 图片或 MP4 视频。')
    }
    const sha256 = await sha256Hex(file)
    const created = await apiRequest<{
      session: { id: string; project_asset_ref: null | { project_id: string; asset_version: { asset_id: string; version: number } } }
      upload: null | { url: string; method: 'PUT'; headers: Record<string, string> }
    }>(`/platform/v1/projects/${encodeURIComponent(projectId)}/assets/uploads`, {
      method: 'POST',
      headers: mutationHeaders(createMutationKey('strategy-media-upload')),
      body: JSON.stringify({
        filename: file.name,
        declared_mime_type: file.type,
        declared_size_bytes: file.size,
        declared_sha256: sha256,
      }),
    })
    let assetRef = created.session.project_asset_ref?.asset_version
    if (!assetRef) {
      if (!created.upload) throw new Error('素材上传会话没有返回可用地址。')
      const headers = new Headers(created.upload.headers)
      headers.delete('host')
      headers.delete('content-length')
      if (!headers.has('Content-Type')) headers.set('Content-Type', file.type)
      const response = await fetch(created.upload.url, { method: created.upload.method, headers, body: file })
      if (!response.ok) throw new Error(`素材上传失败（HTTP ${response.status}）。`)
      const completed = await apiRequest<{
        project_asset_ref: null | { project_id: string; asset_version: { asset_id: string; version: number } }
      }>(`/platform/v1/projects/${encodeURIComponent(projectId)}/assets/uploads/${encodeURIComponent(created.session.id)}:finalize`, { method: 'POST' })
      assetRef = completed.project_asset_ref?.asset_version
    }
    if (!assetRef) throw new Error('素材已上传，但没有生成可用的 AssetVersion。')
    return apiRequest<MediaUnderstandingArtifact>(
      `/api/media/v1/projects/${encodeURIComponent(projectId)}/understandings`,
      {
        method: 'POST',
        headers: mutationHeaders(createMutationKey('strategy-media-understanding')),
        body: JSON.stringify({ asset_id: assetRef.asset_id, version: assetRef.version }),
      },
    )
  },

  getMediaUnderstanding: (projectId: string, artifactId: string, signal?: AbortSignal) =>
    apiRequest<MediaUnderstandingArtifact>(
      `/api/media/v1/projects/${encodeURIComponent(projectId)}/understandings/${encodeURIComponent(artifactId)}`,
      { signal },
    ),

  getLatestMediaUnderstanding: (projectId: string, assetId: string, version: number, signal?: AbortSignal) =>
    apiRequest<MediaUnderstandingArtifact>(
      `/api/media/v1/projects/${encodeURIComponent(projectId)}/assets/${encodeURIComponent(assetId)}/versions/${version}/understanding`,
      { signal },
    ),

  runExternalResearch: (
    projectId: string,
    request: {
      category?: ResearchArtifact['category']
      purpose?: ResearchRun['purpose']
      source_ref?: ResearchRun['source_ref']
	  run_mode?: ResearchRun['run_mode']
	  input_snapshot_ref?: string
	  input_snapshot?: ProjectContextManifest
      query: string
      document_ids: string[]
      disclosed_fields: string[]
      confirmed: boolean
    },
  ) =>
    apiRequest<ResearchRun>(
      `/platform/v1/projects/${encodeURIComponent(projectId)}/knowledge/research-runs`,
      { method: 'POST', body: JSON.stringify(request) },
    ),

  listResearchRuns: (projectId: string, signal?: AbortSignal) =>
    apiRequest<{ items: ResearchRun[] }>(
      `/platform/v1/projects/${encodeURIComponent(projectId)}/knowledge/research-runs?limit=20`,
      { signal },
    ),

  listResearchArtifacts: (projectId: string, category = 'all', signal?: AbortSignal) =>
    apiRequest<{ items: ResearchArtifact[] }>(
      `/platform/v1/projects/${encodeURIComponent(projectId)}/knowledge/research-artifacts?category=${encodeURIComponent(category)}&limit=100`,
      { signal },
    ),

  getResearchRun: (projectId: string, researchRunId: string, signal?: AbortSignal) =>
    apiRequest<ResearchRun>(
      `/platform/v1/projects/${encodeURIComponent(projectId)}/knowledge/research-runs/${encodeURIComponent(researchRunId)}`,
      { signal },
    ),

	listResearchFindings: (projectId: string, researchRunId: string, signal?: AbortSignal) =>
		apiRequest<{ items: ResearchRun['findings'] }>(
			`/platform/v1/projects/${encodeURIComponent(projectId)}/knowledge/research-runs/${encodeURIComponent(researchRunId)}/findings`,
			{ signal },
		),

	getResearchReport: (projectId: string, researchRunId: string, signal?: AbortSignal) =>
		apiRequest<ResearchArtifact>(
			`/platform/v1/projects/${encodeURIComponent(projectId)}/knowledge/research-runs/${encodeURIComponent(researchRunId)}/report`,
			{ signal },
		),

  cancelResearchRun: (projectId: string, researchRunId: string) =>
    apiRequest<KnowledgeJobControl>(
      `/platform/v1/projects/${encodeURIComponent(projectId)}/knowledge/research-runs/${encodeURIComponent(researchRunId)}/cancel`,
      { method: 'POST' },
    ),

  retryResearchRun: (projectId: string, researchRunId: string) =>
    apiRequest<ResearchRun>(
      `/platform/v1/projects/${encodeURIComponent(projectId)}/knowledge/research-runs/${encodeURIComponent(researchRunId)}/retry`,
      { method: 'POST' },
    ),

	listResearchAdoptionProposals: (workspaceId: string, researchRunId: string, signal?: AbortSignal) =>
		apiRequest<{ items: ArtifactProposal[] }>(
			`/api/strategy/v1/workspaces/${encodeURIComponent(workspaceId)}/research-adoption-proposals?run_id=${encodeURIComponent(researchRunId)}`,
			{ signal },
		),

	applyResearchAdoptionProposal: (
		proposalId: string,
		expectedVersion: number,
		operations?: BriefPatchOperation[],
	) => apiRequest<{ proposal: ArtifactProposal; brief_draft?: BriefDraft; strategy_draft?: StrategyDraft }>(
		`/api/strategy/v1/research-adoption-proposals/${encodeURIComponent(proposalId)}:apply`,
		{
			method: 'POST',
			headers: { 'Idempotency-Key': createMutationKey('research-proposal-apply') },
			body: JSON.stringify({ expected_version: expectedVersion, operations }),
		},
	),

	remapResearchAdoptionProposal: (proposalId: string, expectedVersion: number) =>
		apiRequest<ArtifactProposal>(
			`/api/strategy/v1/research-adoption-proposals/${encodeURIComponent(proposalId)}:remap`,
			{
				method: 'POST',
				headers: { 'Idempotency-Key': createMutationKey('research-proposal-remap') },
				body: JSON.stringify({ expected_version: expectedVersion }),
			},
		),

	ignoreResearchAdoptionProposal: (proposalId: string, expectedVersion: number) =>
		apiRequest<ArtifactProposal>(
			`/api/strategy/v1/research-adoption-proposals/${encodeURIComponent(proposalId)}:ignore`,
			{
				method: 'POST',
				headers: { 'Idempotency-Key': createMutationKey('research-proposal-ignore') },
				body: JSON.stringify({ expected_version: expectedVersion }),
			},
		),
}

async function sha256Hex(blob: Blob) {
  if (!globalThis.crypto?.subtle) throw new Error('当前浏览器不支持素材 SHA-256 校验。')
  const digest = await globalThis.crypto.subtle.digest('SHA-256', await blob.arrayBuffer())
  return Array.from(new Uint8Array(digest), byte => byte.toString(16).padStart(2, '0')).join('')
}
