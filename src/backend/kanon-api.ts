import type {
  ApiAgencyWorkbench,
  ApiArtifact,
  ApiAssetVersionPointer,
  ApiBusinessTask,
  ApiCommerceTemplateId,
  ApiCreativeSourceOption,
  ApiGenerationJob,
  ApiPreparedCommercePreroll,
  ApiProject,
  ApiProviderCapabilities,
} from '../data/api.js'
import {
  apiRequest,
  buildAgencyWorkbench,
  createBackendProject,
  enrichProjectRecord,
  loadWorkspaceBootstrap,
  toProjectRecord,
  type BackendProject,
} from './platform.js'

type ListResponse<T> = { items: T[] }

type StrategyPackage = {
  package_id: string
  version: number
  status: string
  content_hash: string
  published_at?: string
  snapshot?: Record<string, unknown>
}

type ProjectAsset = {
  asset: {
    id: string
    status: string
    created_at?: string
    updated_at: string
  }
  version: {
    version: number
    mime_type: string
    source_type: string
    sha256: string
    provider_job_id?: string
    created_at: string
  }
  created_at?: string
}

type AssetVersionRef = {
  asset_id: string
  version: number
}

type SignedRequest = {
  url: string
  method: 'GET' | 'PUT'
  headers: Record<string, string>
  expires_at: string
}

type UploadSession = {
  id: string
  project_asset_ref: null | {
    project_id: string
    asset_version: AssetVersionRef
  }
}

type CreateUploadResponse = {
  session: UploadSession
  upload: SignedRequest | null
}

type ProviderJob = {
  id: string
  kind: string
  project_id: string
  execution_status: 'queued' | 'running' | 'succeeded' | 'failed' | 'cancelled'
  provider_status: string
  progress: number
  project_asset_refs: Array<{
    project_id: string
    asset_version: {
      asset_id: string
      version: number
    }
  }>
  error: null | {
    code: string
    message: string
    retryable: boolean
  }
  version: number
  created_at: string
  updated_at: string
}

type StrategyWorkspace = {
  id: string
  project_id: string
  name: string
  is_primary: boolean
}

type StrategyConversationBundle = {
  conversation: { id: string }
  task: { id: string; brief_id: string }
  brief_draft: StrategyBriefDraft
}

type StrategyAgentTask = {
  id: string
  status: 'dispatch_pending' | 'queued' | 'running' | 'succeeded' | 'failed' | 'cancelled'
  error?: { code: string; message: string }
  created_at: string
  updated_at: string
  version: number
}

type StrategySendMessageResult = {
  agent_task: StrategyAgentTask
}

type StrategyBriefDraft = {
  brief_id: string
  version: number
  document: Record<string, unknown>
  completeness: {
    ready: boolean
    blockers: Array<{ field_path?: string; message?: string }>
    warnings: Array<{ field_path?: string; message?: string }>
  }
  created_at: string
  updated_at: string
}

type StrategyBriefVersion = {
  brief_id: string
  version: number
  snapshot: Record<string, unknown>
  content_hash: string
  source_draft_version: number
  confirmed_at: string
}

const jobProjects = new Map<string, string>()

export async function loadKanonAgencyWorkbench(): Promise<ApiAgencyWorkbench> {
  const bootstrap = await loadWorkspaceBootstrap()
  const [projects, projectArtifacts] = await Promise.all([
    Promise.all(
      bootstrap.projects.map(async project => {
        const base = toProjectRecord(project, bootstrap.identity)
        return enrichProjectRecord(base).catch(() => base)
      }),
    ),
    Promise.all(
      bootstrap.projects.map(project => listKanonArtifacts(project.id).catch(() => [])),
    ),
  ])
  const workbench = buildAgencyWorkbench(bootstrap.identity, bootstrap.projects, projects)
  return {
    ...workbench,
    assetVersionPointers: projectArtifacts
      .flat()
      .filter((artifact): artifact is ApiArtifact & { kind: 'image' | 'video' } =>
        artifact.status === 'ready' && (artifact.kind === 'image' || artifact.kind === 'video'),
      )
      .map(artifact => artifactVersionPointer(
        bootstrap.identity.organization.id,
        bootstrap.identity.user?.display_name ?? bootstrap.identity.organization.name,
        artifact,
      )),
  }
}

export async function listKanonProjects(): Promise<ApiProject[]> {
  const bootstrap = await loadWorkspaceBootstrap()
  const projects = await Promise.all(
    bootstrap.projects.map(async project => {
      const base = toProjectRecord(project, bootstrap.identity)
      return enrichProjectRecord(base).catch(() => base)
    }),
  )
  return projects.map((project, index) => {
    const source = bootstrap.projects[index]
    return {
      id: project.id,
      name: project.name,
      brand: project.brand,
      objective: project.goal,
      runtime: {
        code: project.code,
        product: project.product,
        stage: project.stage,
        progress: project.progress,
        status: project.status === '已完成' ? 'completed' : 'active',
        owner: project.owner,
        budget: project.budget,
        currency: project.currency,
        timezone: project.timezone,
      },
      version: source.project_context_version,
      createdAt: source.created_at,
      updatedAt: source.updated_at,
    }
  })
}

export async function createKanonProject(
  input: Pick<ApiProject, 'name' | 'brand' | 'objective'>,
): Promise<ApiProject> {
  const created = await createBackendProject(input)
  return mapBackendProject(created, input.objective)
}

export async function createKanonBrief(
  projectId: string,
  prompt: string,
): Promise<{ job: ApiGenerationJob; artifact: ApiArtifact }> {
  const encodedProjectId = encodeURIComponent(projectId)
  const workspaceResponse = await apiRequest<ListResponse<StrategyWorkspace>>(
    `/api/strategy/v1/projects/${encodedProjectId}/workspaces`,
  )
  let workspace = workspaceResponse.items.find(item => item.is_primary) ?? workspaceResponse.items[0]
  if (!workspace) {
    workspace = await apiRequest<StrategyWorkspace>('/api/strategy/v1/workspaces', {
      method: 'POST',
      headers: {'Idempotency-Key': browserIdempotencyKey('brief-workspace')},
      body: JSON.stringify({project_id: projectId, name: '需求中心'}),
    })
  }
  const bundle = await apiRequest<StrategyConversationBundle>('/api/strategy/v1/conversations', {
    method: 'POST',
    headers: {'Idempotency-Key': browserIdempotencyKey('brief-conversation')},
    body: JSON.stringify({project_id: projectId, workspace_id: workspace.id}),
  })
  const sent = await apiRequest<StrategySendMessageResult>(
    `/api/strategy/v1/conversations/${encodeURIComponent(bundle.conversation.id)}/messages`,
    {
      method: 'POST',
      headers: {'Idempotency-Key': browserIdempotencyKey('brief-message')},
      body: JSON.stringify({content: prompt.trim()}),
    },
  )
  const task = await waitForStrategyAgentTask(sent.agent_task.id)
  if (task.status !== 'succeeded') {
    throw new Error(task.error?.message ?? `Brief 提取任务 ${task.status}。`)
  }
  const draft = await apiRequest<StrategyBriefDraft>(
    `/api/strategy/v1/tasks/${encodeURIComponent(bundle.task.id)}/brief-draft`,
  )
  const artifact = mapBriefDraftArtifact(projectId, bundle.task.id, draft)
  return {
    job: {
      id: task.id,
      projectId,
      artifactKind: 'brief',
      status: 'succeeded',
      model: 'cookies.strategy.text',
      artifactId: artifact.id,
      version: task.version,
      createdAt: task.created_at,
      updatedAt: task.updated_at,
    },
    artifact,
  }
}

export async function confirmKanonBrief(artifact: ApiArtifact): Promise<ApiArtifact> {
  if (!artifact.briefTaskId || !artifact.briefDraftVersion) {
    throw new Error('当前 Brief 缺少任务与草稿版本，无法确认。')
  }
  const currentDraft = await apiRequest<StrategyBriefDraft>(
    `/api/strategy/v1/tasks/${encodeURIComponent(artifact.briefTaskId)}/brief-draft`,
  )
  const operations = confirmedBriefOperations(currentDraft.document)
  if (!operations.length) {
    throw new Error('当前 Brief 没有可确认的结构化字段，请先补充需求。')
  }
  const confirmedDraft = await apiRequest<StrategyBriefDraft>(
    `/api/strategy/v1/tasks/${encodeURIComponent(artifact.briefTaskId)}/brief-draft`,
    {
      method: 'PATCH',
      headers: {'Idempotency-Key': browserIdempotencyKey('brief-confirm-fields')},
      body: JSON.stringify({
        contract_version: 'strategy-brief-patch/v2',
        expected_version: currentDraft.version,
        operations,
      }),
    },
  )
  const version = await apiRequest<StrategyBriefVersion>(
    `/api/strategy/v1/tasks/${encodeURIComponent(artifact.briefTaskId)}/brief:confirm`,
    {
      method: 'POST',
      headers: {'Idempotency-Key': browserIdempotencyKey('brief-confirm')},
      body: JSON.stringify({expected_version: confirmedDraft.version}),
    },
  )
  return {
    ...artifact,
    id: version.brief_id,
    status: 'ready',
    content: summarizeBriefDocument(version.snapshot),
    version: version.version,
    updatedAt: version.confirmed_at,
  }
}

export async function attachKanonBriefProductAsset(
  artifact: ApiArtifact,
  asset: AssetVersionRef,
): Promise<ApiArtifact> {
  if (!artifact.briefTaskId) {
    throw new Error('当前 Brief 缺少任务引用，无法绑定商品图。')
  }
  const currentDraft = await apiRequest<StrategyBriefDraft>(
    `/api/strategy/v1/tasks/${encodeURIComponent(artifact.briefTaskId)}/brief-draft`,
  )
  const existing = Array.isArray(asRecord(currentDraft.document.product)?.asset_refs)
    ? asRecord(currentDraft.document.product)?.asset_refs as unknown[]
    : []
  const nextAssets = [
    ...existing.filter(value => {
      const ref = asRecord(value)
      return ref?.asset_id !== asset.asset_id || ref?.version !== asset.version
    }),
    asset,
  ]
  const updated = await apiRequest<StrategyBriefDraft>(
    `/api/strategy/v1/tasks/${encodeURIComponent(artifact.briefTaskId)}/brief-draft`,
    {
      method: 'PATCH',
      headers: {'Idempotency-Key': browserIdempotencyKey('brief-product-asset')},
      body: JSON.stringify({
        contract_version: 'strategy-brief-patch/v2',
        expected_version: currentDraft.version,
        operations: [{
          op: 'set',
          field_path: 'product.asset_refs',
          value: nextAssets,
        }],
      }),
    },
  )
  return {
    ...artifact,
    content: summarizeBriefDocument(updated.document),
    briefDraftVersion: updated.version,
    version: updated.version,
    updatedAt: updated.updated_at,
  }
}

function confirmedBriefOperations(document: Record<string, unknown>) {
  const paths: Array<[string, unknown]> = [
    ['brand.name', asRecord(document.brand)?.name],
    ['product.name', asRecord(document.product)?.name],
    ['product.category', asRecord(document.product)?.category],
    ['product.selling_points', asRecord(document.product)?.selling_points],
    ['product.evidence', asRecord(document.product)?.evidence],
    ['product.asset_refs', asRecord(document.product)?.asset_refs],
    ['industry', document.industry],
    ['region', document.region],
    ['language', document.language],
    ['campaign.objective', asRecord(document.campaign)?.objective],
    ['audience.primary', asRecord(document.audience)?.primary],
    ['proposition', document.proposition],
    ['channels', document.channels],
    ['budget.total', asRecord(document.budget)?.total],
    ['schedule.window', asRecord(document.schedule)?.window],
    ['constraints', document.constraints],
    ['measurement.primary_kpi', asRecord(document.measurement)?.primary_kpi],
    ['reference_ids', document.reference_ids],
    ['creative.tone', asRecord(document.creative)?.tone],
    ['creative.mandatory_elements', asRecord(document.creative)?.mandatory_elements],
    ['creative.prohibited_claims', asRecord(document.creative)?.prohibited_claims],
  ]
  const operations: Array<{op: 'set'; field_path: string; value: string | unknown[]}> = []
  for (const [fieldPath, value] of paths) {
    if (typeof value === 'string' && value.trim()) {
      operations.push({op: 'set', field_path: fieldPath, value})
      continue
    }
    if (Array.isArray(value)) {
      if (value.length || fieldPath === 'constraints' || fieldPath === 'reference_ids') {
        operations.push({op: 'set', field_path: fieldPath, value})
      }
    }
  }
  return operations
}

async function waitForStrategyAgentTask(agentTaskId: string): Promise<StrategyAgentTask> {
  const terminal = new Set<StrategyAgentTask['status']>(['succeeded', 'failed', 'cancelled'])
  for (let attempt = 0; attempt < 120; attempt += 1) {
    const task = await apiRequest<StrategyAgentTask>(
      `/api/strategy/v1/agent-tasks/${encodeURIComponent(agentTaskId)}`,
    )
    if (terminal.has(task.status)) return task
    await new Promise(resolve => window.setTimeout(resolve, 1000))
  }
  throw new Error('Brief 提取超过 120 秒仍未完成，请稍后重试。')
}

function mapBriefDraftArtifact(
  projectId: string,
  taskId: string,
  draft: StrategyBriefDraft,
): ApiArtifact {
  return {
    id: draft.brief_id,
    projectId,
    kind: 'brief',
    status: 'draft',
    content: summarizeBriefDocument(draft.document),
    briefTaskId: taskId,
    briefDraftVersion: draft.version,
    version: draft.version,
    createdAt: draft.created_at,
    updatedAt: draft.updated_at,
  }
}

function summarizeBriefDocument(document: Record<string, unknown>): string {
  const brand = asRecord(document.brand)
  const product = asRecord(document.product)
  const campaign = asRecord(document.campaign)
  const audience = asRecord(document.audience)
  const creative = asRecord(document.creative)
  const values = [
    brand?.name ? `品牌：${brand.name}` : '',
    product?.name ? `商品：${product.name}` : '',
    campaign?.objective ? `目标：${campaign.objective}` : '',
    audience?.primary ? `受众：${audience.primary}` : '',
    document.proposition ? `核心主张：${document.proposition}` : '',
    Array.isArray(product?.selling_points) && product.selling_points.length
      ? `卖点：${product.selling_points.join('、')}`
      : '',
    Array.isArray(creative?.prohibited_claims) && creative.prohibited_claims.length
      ? `禁止项：${creative.prohibited_claims.join('、')}`
      : '',
  ].filter(Boolean)
  return values.join('\n') || 'Brief 已提取，请检查必填字段后确认。'
}

export async function listKanonArtifacts(projectId: string): Promise<ApiArtifact[]> {
  const encodedProjectId = encodeURIComponent(projectId)
  const [packageResult, briefResult, assetResult] = await Promise.allSettled([
    apiRequest<ListResponse<StrategyPackage>>(
      `/api/strategy/v1/projects/${encodedProjectId}/strategy-packages`,
    ),
    apiRequest<ListResponse<StrategyBriefVersion>>(
      `/api/strategy/v1/projects/${encodedProjectId}/brief-versions`,
    ),
    apiRequest<ListResponse<ProjectAsset>>(
      `/platform/v1/projects/${encodedProjectId}/assets?limit=100`,
    ),
  ])

  const packages = packageResult.status === 'fulfilled' ? packageResult.value.items : []
  const briefs = briefResult.status === 'fulfilled' ? briefResult.value.items : []
  const assets = assetResult.status === 'fulfilled' ? assetResult.value.items : []
  const briefArtifacts = briefs.map<ApiArtifact>(item => ({
    id: `${item.brief_id}:v${item.version}`,
    projectId,
    kind: 'brief',
    status: 'ready',
    content: summarizeBriefDocument(item.snapshot),
    version: item.version,
    createdAt: item.confirmed_at,
    updatedAt: item.confirmed_at,
  }))
  const strategyArtifacts = packages.map<ApiArtifact>(item => {
    const timestamp = item.published_at ?? new Date(0).toISOString()
    return {
      id: item.package_id,
      projectId,
      kind: 'document',
      status: item.status === 'archived' ? 'archived' : 'ready',
      content: summarizeStrategyPackage(item),
      version: item.version,
      createdAt: timestamp,
      updatedAt: timestamp,
    }
  })
  const assetArtifacts = assets.map<ApiArtifact>(item => {
    const kind = artifactKindFromMime(item.version.mime_type)
    const timestamp = item.version.created_at || item.asset.updated_at
    if (item.version.provider_job_id) {
      jobProjects.set(item.version.provider_job_id, projectId)
    }
    return {
      id: item.asset.id,
      projectId,
      kind,
      status: item.asset.status === 'ready' ? 'ready' : 'draft',
      content: assetContentUrl(projectId, item.asset.id, item.version.version),
      sourceJobId: item.version.provider_job_id,
      version: item.version.version,
      createdAt: item.asset.created_at ?? item.created_at ?? timestamp,
      updatedAt: item.asset.updated_at || timestamp,
    }
  })

  return [...briefArtifacts, ...strategyArtifacts, ...assetArtifacts]
    .sort((left, right) => left.createdAt.localeCompare(right.createdAt))
}

export async function listKanonCommercePrerollSources(
  projectId: string,
): Promise<ApiCreativeSourceOption[]> {
  const response = await apiRequest<ListResponse<ApiCreativeSourceOption>>(
    `/api/creative/v1/projects/${encodeURIComponent(projectId)}/commerce-preroll/sources`,
  )
  return response.items
}

export async function prepareKanonCommercePreroll(
  projectId: string,
  source: ApiCreativeSourceOption,
  templateId: ApiCommerceTemplateId,
): Promise<ApiPreparedCommercePreroll> {
  const productAsset = source.product.product_asset_refs[0]
  return apiRequest<ApiPreparedCommercePreroll>(
    `/api/creative/v1/projects/${encodeURIComponent(projectId)}/commerce-preroll:prepare`,
    {
      method: 'POST',
      body: JSON.stringify({
        source_ref: source.source_ref,
        template_ref: {
          template_id: templateId,
          template_version: 1,
        },
        ...(productAsset ? { product_asset_ref: productAsset } : {}),
      }),
    },
  )
}

export async function listKanonJobs(projectId: string): Promise<ApiGenerationJob[]> {
  const artifacts = await listKanonArtifacts(projectId)
  return artifacts
    .filter((artifact): artifact is ApiArtifact & { sourceJobId: string } => Boolean(artifact.sourceJobId))
    .map(artifact => {
      jobProjects.set(artifact.sourceJobId, projectId)
      return {
        id: artifact.sourceJobId,
        projectId,
        artifactKind: artifact.kind,
        status: 'succeeded',
        model: artifact.kind === 'video' ? 'cookies.video.standard' : 'cookies.image.standard',
        artifactId: artifact.id,
        version: artifact.version,
        createdAt: artifact.createdAt,
        updatedAt: artifact.updatedAt,
      }
    })
}

export async function getKanonJob(jobId: string): Promise<ApiGenerationJob> {
  const projectId = jobProjects.get(jobId)
  if (!projectId) {
    throw new Error('当前页面没有该模型作业所属的 Project，无法读取作业状态。')
  }
  const job = await apiRequest<ProviderJob>(
    `/platform/v1/projects/${encodeURIComponent(projectId)}/model/jobs/${encodeURIComponent(jobId)}`,
  )
  return mapProviderJob(job)
}

export async function createKanonMedia(
  projectId: string,
  kind: 'image' | 'video',
  prompt: string,
  briefId: string,
): Promise<ApiGenerationJob> {
  const bootstrap = await loadWorkspaceBootstrap()
  const project = bootstrap.projects.find(item => item.id === projectId)
  if (!project) {
    throw new Error(`当前身份无法访问 Project ${projectId}。`)
  }
  const capability = kind === 'video' ? 'video.generate' : 'image.generate'
  const modelAlias = kind === 'video' ? 'cookies.video.standard' : 'cookies.image.standard'
  const input = kind === 'video'
    ? {
        prompt,
        duration_seconds: 6,
        aspect_ratio: '9:16',
        resolution: '720p',
      }
    : {
        prompt,
        width: 1024,
        height: 1024,
      }
  const job = await apiRequest<ProviderJob>(
    `/platform/v1/projects/${encodeURIComponent(projectId)}/model/jobs`,
    {
      method: 'POST',
      headers: {
        'Idempotency-Key': browserIdempotencyKey(kind),
      },
      body: JSON.stringify({
        capability,
        model_alias: modelAlias,
        input,
        project_context_version: project.project_context_version,
        source_system: 'kanon-frontend',
        source_task_id: briefId,
      }),
    },
  )
  jobProjects.set(job.id, projectId)
  return mapProviderJob(job, kind)
}

const guerlainWindowRevealFrames = [
  {
    role: 'first_frame',
    publicPath: '/assets/guerlain-youth-watery-oil-frosted-start.jpg',
    filename: 'guerlain-youth-watery-oil-frosted-start.jpg',
  },
  {
    role: 'last_frame',
    publicPath: '/assets/guerlain-youth-watery-oil-tail.jpg',
    filename: 'guerlain-youth-watery-oil-tail.jpg',
  },
] as const

export async function createKanonCommercePrerollVideo(
  projectId: string,
  prompt: string,
  briefId: string,
): Promise<ApiGenerationJob> {
  const conditioningAssets = await ensureGuerlainWindowRevealFrames(projectId)
  const bootstrap = await loadWorkspaceBootstrap()
  const project = bootstrap.projects.find(item => item.id === projectId)
  if (!project) {
    throw new Error(`当前身份无法访问 Project ${projectId}。`)
  }

  const job = await apiRequest<ProviderJob>(
    `/platform/v1/projects/${encodeURIComponent(projectId)}/model/jobs`,
    {
      method: 'POST',
      headers: {
        'Idempotency-Key': browserIdempotencyKey('video'),
      },
      body: JSON.stringify({
        capability: 'video.generate',
        model_alias: 'cookies.video.standard',
        input: {
          prompt,
          duration_seconds: 6,
          aspect_ratio: '9:16',
          resolution: '720p',
          audio_policy: 'silent',
          input_mode: 'first_last_frame',
          conditioning_assets: conditioningAssets.map(frame => ({
            role: frame.role,
            reference: {
              project_id: projectId,
              asset_version: frame.assetVersion,
            },
          })),
        },
        project_context_version: project.project_context_version,
        source_system: 'creative-commerce-preroll',
        source_task_id: briefId,
      }),
    },
  )
  jobProjects.set(job.id, projectId)
  return mapProviderJob(job, 'video')
}

export async function createKanonPreparedCommercePrerollVideo(
  projectId: string,
  prompt: string,
  sourceId: string,
  productAsset: AssetVersionRef,
): Promise<ApiGenerationJob> {
  const bootstrap = await loadWorkspaceBootstrap()
  const project = bootstrap.projects.find(item => item.id === projectId)
  if (!project) {
    throw new Error(`当前身份无法访问 Project ${projectId}。`)
  }
  const job = await apiRequest<ProviderJob>(
    `/platform/v1/projects/${encodeURIComponent(projectId)}/model/jobs`,
    {
      method: 'POST',
      headers: {
        'Idempotency-Key': browserIdempotencyKey('video'),
      },
      body: JSON.stringify({
        capability: 'video.generate',
        model_alias: 'cookies.video.standard',
        input: {
          prompt,
          duration_seconds: 6,
          aspect_ratio: '9:16',
          resolution: '720p',
          audio_policy: 'silent',
          input_mode: 'reference_image',
          conditioning_assets: [{
            role: 'reference_image',
            reference: {
              project_id: projectId,
              asset_version: productAsset,
            },
          }],
        },
        project_context_version: project.project_context_version,
        source_system: 'creative-commerce-preroll',
        source_task_id: sourceId,
      }),
    },
  )
  jobProjects.set(job.id, projectId)
  return mapProviderJob(job, 'video')
}

export async function getKanonCapabilities(): Promise<ApiProviderCapabilities> {
  const response = await apiRequest<{
    provider: string
    status: 'configured' | 'not_configured'
    capabilities: Array<{
      capability: string
      model_alias: string
      upstream_model: string
      available: boolean
      connection_type?: string
    }>
    credential?: { source?: 'environment' | 'workspace'; masked_api_key?: string }
    checked_at: string
  }>('/platform/v1/provider/capabilities')
  return {
    provider: response.provider,
    status: response.status,
    capabilities: response.capabilities.map(item => ({
      capability: item.capability,
      model: `${item.model_alias} → ${item.upstream_model}`,
      available: item.available,
      connectionType: item.connection_type,
    })),
    credential: response.credential ? {
      source: response.credential.source,
      maskedApiKey: response.credential.masked_api_key,
    } : undefined,
    checkedAt: response.checked_at,
  }
}

export async function listKanonTasks(projectId: string): Promise<ApiBusinessTask[]> {
  const bootstrap = await loadWorkspaceBootstrap()
  const backendProject = bootstrap.projects.find(project => project.id === projectId)
  if (!backendProject) return []
  const project = await enrichProjectRecord(toProjectRecord(backendProject, bootstrap.identity))
  return project.tasks
}

export function unsupportedKanonWrite(action: string): Error {
  return new Error(`${action}暂时无法完成。请从对应工作区继续，或稍后重试。`)
}

function mapBackendProject(project: BackendProject, objective: string): ApiProject {
  return {
    id: project.id,
    name: project.name,
    brand: project.primary_brand_id ?? '尚未绑定品牌',
    objective,
    runtime: {
      code: project.id,
      product: '项目产品',
      stage: project.status === 'active' ? '进行中' : '准备中',
      progress: 0,
      status: project.status === 'archived' ? 'completed' : 'active',
      owner: project.organization_id,
      budget: 0,
      currency: 'CNY',
      timezone: 'Asia/Shanghai',
    },
    version: project.project_context_version,
    createdAt: project.created_at,
    updatedAt: project.updated_at,
  }
}

function mapProviderJob(job: ProviderJob, requestedKind?: 'image' | 'video'): ApiGenerationJob {
  const asset = job.project_asset_refs.at(-1)?.asset_version
  const kind = requestedKind ?? (job.kind.includes('video') ? 'video' : 'image')
  const status = normalizeJobStatus(job.execution_status, job.provider_status)
  return {
    id: job.id,
    projectId: job.project_id,
    artifactKind: kind,
    status,
    model: kind === 'video' ? 'cookies.video.standard' : 'cookies.image.standard',
    diagnostic: job.error?.message,
    artifactId: asset?.asset_id,
    version: job.version,
    createdAt: job.created_at,
    updatedAt: job.updated_at,
  }
}

function normalizeJobStatus(
  executionStatus: ProviderJob['execution_status'],
  providerStatus: string,
): ApiGenerationJob['status'] {
  if (executionStatus === 'failed' || providerStatus === 'failed' || providerStatus === 'expired') {
    return 'failed'
  }
  if (executionStatus === 'cancelled' || providerStatus === 'cancelled') return 'cancelled'
  if (executionStatus === 'succeeded' || providerStatus === 'succeeded' || providerStatus === 'partially_succeeded') {
    return 'succeeded'
  }
  if (executionStatus === 'running' || providerStatus !== 'submitted') return 'running'
  return 'queued'
}

function summarizeStrategyPackage(item: StrategyPackage): string {
  const strategy = asRecord(item.snapshot?.strategy)
  const brief = asRecord(item.snapshot?.brief)
  const candidates = [
    strategy?.objective,
    strategy?.core_message,
    brief?.objective,
    brief?.summary,
  ].filter((value): value is string => typeof value === 'string' && value.trim().length > 0)
  return candidates[0] ?? `已批准策略包 ${item.package_id} v${item.version}`
}

function asRecord(value: unknown): Record<string, unknown> | undefined {
  return value !== null && typeof value === 'object' && !Array.isArray(value)
    ? value as Record<string, unknown>
    : undefined
}

function artifactKindFromMime(mimeType: string): ApiArtifact['kind'] {
  if (mimeType.startsWith('video/')) return 'video'
  if (mimeType.startsWith('image/')) return 'image'
  return 'document'
}

function assetContentUrl(projectId: string, assetId: string, version: number): string {
  return `/platform/v1/projects/${encodeURIComponent(projectId)}/assets/${encodeURIComponent(assetId)}/versions/${version}/content`
}

function artifactVersionPointer(
  organizationId: string,
  owner: string,
  artifact: ApiArtifact & { kind: 'image' | 'video' },
): ApiAssetVersionPointer {
  const generated = Boolean(artifact.sourceJobId)
  return {
    id: `pointer-${artifact.id}`,
    organizationId,
    projectId: artifact.projectId,
    assetId: artifact.id,
    mediaKind: artifact.kind,
    contentUrl: artifact.content,
    sourceJobId: artifact.sourceJobId,
    workingVersion: artifact.version,
    versions: [{
      version: artifact.version,
      createdBy: generated ? 'Seedance 2.0' : owner,
      sourceTaskId: artifact.sourceJobId ?? artifact.id,
      sourceType: generated ? 'model_generation' : 'manual_edit',
      sourceLabel: generated ? '视频生成任务产物' : '项目素材上传',
      createdAt: artifact.createdAt,
      changeSummary: generated
        ? '模型生成完成并持久化后，自动进入素材检查队列。'
        : '上传素材持久化后，自动进入素材检查队列。',
    }],
    authorization: {
      platforms: [],
      regions: [],
      rightsHolder: '待素材检查确认',
      expiresAt: '1970-01-01T00:00:00.000Z',
      note: '系统不会从生成成功推断投放授权；请在进入交付版本前补齐授权范围。',
    },
    deliveryTarget: {
      platform: '巨量引擎',
      region: '中国大陆',
    },
    owner,
    updatedAt: artifact.updatedAt,
  }
}

function browserIdempotencyKey(kind: string): string {
  const random = globalThis.crypto?.randomUUID?.().replaceAll('-', '') ?? `${Date.now()}`
  return `kanon-${kind}-${random}`.slice(0, 120)
}

async function ensureGuerlainWindowRevealFrames(projectId: string) {
  const encodedProjectId = encodeURIComponent(projectId)
  const existing = await apiRequest<ListResponse<ProjectAsset>>(
    `/platform/v1/projects/${encodedProjectId}/assets?limit=100`,
  )
  return Promise.all(guerlainWindowRevealFrames.map(async frame => {
    const response = await fetch(frame.publicPath)
    if (!response.ok) {
      throw new Error(`无法读取娇兰固定样例帧（HTTP ${response.status}）。`)
    }
    const blob = await response.blob()
    const sha256 = await sha256Hex(blob)
    const matched = existing.items.find(item => item.version.sha256 === sha256)
    if (matched) {
      return {
        role: frame.role,
        assetVersion: {
          asset_id: matched.asset.id,
          version: matched.version.version,
        },
      }
    }

    const created = await apiRequest<CreateUploadResponse>(
      `/platform/v1/projects/${encodedProjectId}/assets/uploads`,
      {
        method: 'POST',
        headers: {
          'Idempotency-Key': browserIdempotencyKey('image'),
        },
        body: JSON.stringify({
          filename: frame.filename,
          declared_mime_type: 'image/jpeg',
          declared_size_bytes: blob.size,
          declared_sha256: sha256,
        }),
      },
    )
    if (!created.upload) {
      const existingRef = created.session.project_asset_ref?.asset_version
      if (existingRef) return { role: frame.role, assetVersion: existingRef }
      throw new Error('娇兰样例帧上传会话未返回上传地址。')
    }
    await putSignedAsset(created.upload, blob)
    const completed = await apiRequest<UploadSession>(
      `/platform/v1/projects/${encodedProjectId}/assets/uploads/${encodeURIComponent(created.session.id)}:finalize`,
      { method: 'POST' },
    )
    const assetVersion = completed.project_asset_ref?.asset_version
    if (!assetVersion) {
      throw new Error('娇兰样例帧已上传，但没有生成可用的 AssetVersion。')
    }
    return { role: frame.role, assetVersion }
  }))
}

export async function ensureKanonGuerlainCommerceFixtureAssets(projectId: string) {
  const frames = await ensureGuerlainWindowRevealFrames(projectId)
  const firstFrame = frames.find(frame => frame.role === 'first_frame')
  const lastFrame = frames.find(frame => frame.role === 'last_frame')
  if (!firstFrame || !lastFrame) {
    throw new Error('娇兰固定样例缺少可用的首帧或尾帧 AssetVersion。')
  }
  return {
    productAsset: lastFrame.assetVersion,
    firstFrame: firstFrame.assetVersion,
    lastFrame: lastFrame.assetVersion,
  }
}

async function putSignedAsset(request: SignedRequest, blob: Blob) {
  const headers = new Headers()
  for (const [name, value] of Object.entries(request.headers)) {
    const normalizedName = name.toLowerCase()
    if (normalizedName !== 'host' && normalizedName !== 'content-length') {
      headers.set(name, value)
    }
  }
  if (!headers.has('Content-Type')) headers.set('Content-Type', 'image/jpeg')
  const response = await fetch(request.url, {
    method: request.method,
    headers,
    body: blob,
  })
  if (!response.ok) {
    throw new Error(`娇兰样例帧上传失败（HTTP ${response.status}）。`)
  }
}

async function sha256Hex(blob: Blob) {
  if (!globalThis.crypto?.subtle) {
    throw new Error('当前浏览器不支持素材 SHA-256 校验。')
  }
  const digest = await globalThis.crypto.subtle.digest('SHA-256', await blob.arrayBuffer())
  return Array.from(new Uint8Array(digest), byte => byte.toString(16).padStart(2, '0')).join('')
}
