import {
  ArrowUpRight,
  Bot,
  BrainCircuit,
  Check,
  CircleCheck,
  FileText,
  Globe2,
  Image as ImageIcon,
  LoaderCircle,
  Paperclip,
  Send,
  Sparkles,
  Video,
  X,
} from 'lucide-react'
import { useEffect, useMemo, useRef, useState, type FormEvent } from 'react'
import {
  buildConversationLens,
  compactDocumentTitle,
  conversationSearchRunsByMessage,
  conversationSourceDocuments,
  intakeMissingLabel,
} from './strategyConversationModel'
import {
  clearWorkspaceSessionValue,
  readWorkspaceSessionValue,
  writeWorkspaceSessionValue,
} from './workspace/workspaceSessionState'
import type {
  BriefDraft,
  BriefVersion,
  ConversationCapabilities,
  CreativeIntakeV4,
  KnowledgeDocument,
  MediaUnderstandingArtifact,
  Message,
  MessageContentBlock,
  MessageRequestedPolicy,
  ResearchRun,
} from './types'

type ViralRemakeResult = { intake: CreativeIntakeV4; taskId?: string }

type Props = {
  brief: BriefDraft | null
  briefVersion: BriefVersion | null
  busy: string
  conversationCapabilities: ConversationCapabilities | null
  draftStorageKey: string
  documents: KnowledgeDocument[]
  mediaArtifacts: MediaUnderstandingArtifact[]
  messages: Message[]
  notice: string
  pending: boolean
  researchRuns: ResearchRun[]
  onConfirmRequirement: () => Promise<boolean>
  onOpenBrief: () => void
  onOpenFullStrategy: () => void
  onReadyViralRemake: (taskId: string) => void
  onSend: (
    content: string,
    documents?: KnowledgeDocument[],
    media?: MediaUnderstandingArtifact[],
    requestedPolicy?: MessageRequestedPolicy,
  ) => Promise<boolean>
  onStartViralRemake: () => Promise<ViralRemakeResult | null>
  onUploadDocument: (file: File) => Promise<KnowledgeDocument | null>
  onUploadMedia: (file: File) => Promise<MediaUnderstandingArtifact | null>
}

type ConversationComposerDraft = {
  attachedDocumentIds: string[]
  attachedMediaIds: string[]
  content: string
  deepReasoning: boolean
  webSearch: boolean
}

const starterPrompts = [
  '我有一条参考视频，想保留节奏结构但做成原创版本',
  '我们要推广一个新品，目标是先让核心人群理解它的价值',
  '我有一份内部 PDF，请先帮我提炼可用于创作的事实',
]

export function StrategyConversationPane({
  brief,
  briefVersion,
  busy,
  conversationCapabilities,
  draftStorageKey,
  documents,
  mediaArtifacts,
  messages,
  notice,
  onConfirmRequirement,
  onOpenBrief,
  onOpenFullStrategy,
  onReadyViralRemake,
  onSend,
  onStartViralRemake,
  onUploadDocument,
  onUploadMedia,
  pending,
  researchRuns = [],
}: Props) {
  const [restoredDraft] = useState<ConversationComposerDraft>(() => readConversationComposerDraft(draftStorageKey) ?? {
    attachedDocumentIds: [],
    attachedMediaIds: [],
    content: '',
    deepReasoning: false,
    webSearch: false,
  })
  const [content, setContent] = useState(restoredDraft.content)
  const [feedback, setFeedback] = useState('')
  const [deepReasoning, setDeepReasoning] = useState(restoredDraft.deepReasoning)
  const [webSearch, setWebSearch] = useState(restoredDraft.webSearch)
  const [attachedDocumentIds, setAttachedDocumentIds] = useState<string[]>(restoredDraft.attachedDocumentIds)
  const [attachedMediaIds, setAttachedMediaIds] = useState<string[]>(restoredDraft.attachedMediaIds)
  const [streamingAssistantIds, setStreamingAssistantIds] = useState<Set<string>>(() => new Set())
  const listRef = useRef<HTMLDivElement>(null)
  const textareaRef = useRef<HTMLTextAreaElement>(null)
  const seenMessageIdsRef = useRef<Set<string> | null>(null)
  const hadPendingTurnRef = useRef(false)
  const sourceDocuments = conversationSourceDocuments(brief, documents)
  const lens = buildConversationLens(brief, sourceDocuments)
  const locked = Boolean(briefVersion)
  const strategyReadiness = briefVersion?.full_strategy_readiness
  const strategyReady = Boolean(strategyReadiness?.ready)
  const strategyBlocker = strategyReadiness?.blockers[0]
  const attachedDocuments = attachedDocumentIds.flatMap(id => {
    const document = documents.find(value => value.id === id)
    return document ? [document] : []
  })
  const attachedMedia = attachedMediaIds.flatMap(id => {
    const artifact = mediaArtifacts.find(value => value.id === id)
    return artifact ? [artifact] : []
  })
  const documentsReady = attachedDocuments.every(document => document.status === 'ready' || document.status === 'partial')
  const mediaReady = attachedMedia.every(artifact => artifact.status === 'ready' || artifact.status === 'partial')
  const attachmentsReady = documentsReady && mediaReady
  const pendingPolicy = [...messages].reverse().find(message => message.role === 'user')?.requested_policy
  const conversationSearchByMessage = useMemo(
    () => conversationSearchRunsByMessage(researchRuns),
    [researchRuns],
  )

  useEffect(() => {
    const list = listRef.current
    if (list) list.scrollTop = list.scrollHeight
  }, [messages, pending])

  useEffect(() => {
    if (pending) hadPendingTurnRef.current = true
  }, [pending])

  useEffect(() => {
    if (seenMessageIdsRef.current === null) {
      seenMessageIdsRef.current = new Set(messages.map(message => message.id))
      return
    }
    const seen = seenMessageIdsRef.current
    const newAssistantIds: string[] = []
    for (const message of messages) {
      if (!seen.has(message.id) && message.role === 'assistant' && hadPendingTurnRef.current) {
        newAssistantIds.push(message.id)
      }
      seen.add(message.id)
    }
    if (!newAssistantIds.length) return
    hadPendingTurnRef.current = false
    setStreamingAssistantIds(current => {
      const next = new Set(current)
      newAssistantIds.forEach(id => next.add(id))
      return next
    })
  }, [messages])

  useEffect(() => {
    const textarea = textareaRef.current
    if (!textarea) return
    textarea.style.height = 'auto'
    textarea.style.height = `${Math.min(Math.max(textarea.scrollHeight, 82), 170)}px`
  }, [content])

  useEffect(() => {
    const value: ConversationComposerDraft = {
      attachedDocumentIds,
      attachedMediaIds,
      content,
      deepReasoning,
      webSearch,
    }
    if (!content && !attachedDocumentIds.length && !attachedMediaIds.length && !deepReasoning && !webSearch) {
      clearWorkspaceSessionValue(draftStorageKey)
      return
    }
    writeWorkspaceSessionValue(draftStorageKey, value)
  }, [attachedDocumentIds, attachedMediaIds, content, deepReasoning, draftStorageKey, webSearch])

  useEffect(() => {
    if (!conversationCapabilities?.deep_reasoning.available) setDeepReasoning(false)
    if (!conversationCapabilities?.web_search.available) setWebSearch(false)
  }, [conversationCapabilities])

  const submit = async (event: FormEvent) => {
    event.preventDefault()
    const value = content.trim()
    if (!value && !attachedDocuments.length && !attachedMedia.length) return
    if (!attachmentsReady) {
      setFeedback('请等待附件解析完成；这样 Agent 读取的是正文与来源定位，而不是文件名占位符。')
      return
    }
    setFeedback('')
    const requestedPolicy: MessageRequestedPolicy | undefined = deepReasoning || webSearch
      ? {
          ...(deepReasoning ? { reasoning_mode: 'deep' as const } : {}),
          ...(webSearch ? { web_search: 'allowed' as const } : {}),
        }
      : undefined
    const sent = await onSend(value, attachedDocuments, attachedMedia, requestedPolicy)
    if (sent) {
      clearWorkspaceSessionValue(draftStorageKey)
      setContent('')
      setAttachedDocumentIds([])
      setAttachedMediaIds([])
      setDeepReasoning(false)
      setWebSearch(false)
    }
  }

  const upload = async (file: File | undefined) => {
    if (!file) return
    setFeedback('')
    const document = await onUploadDocument(file)
    if (document) {
      setAttachedDocumentIds(current => current.includes(document.id) ? current : [...current, document.id])
      setFeedback(document.status === 'ready'
        ? `${file.name} 已有可用解析结果，本次直接复用 ${document.chunk_count} 个来源片段。`
        : `${file.name} 已进入解析队列；完成后会随下一条消息一起发送，并保留 chunk 来源。`)
    }
  }

  const uploadMedia = async (file: File | undefined) => {
    if (!file) return
    setFeedback('')
    const artifact = await onUploadMedia(file)
    if (artifact) {
      setAttachedMediaIds(current => current.includes(artifact.id) ? current : [...current, artifact.id])
      setFeedback(`${file.name} 已进入理解队列；只有带时间点或画面定位的直接证据会进入下一轮对话。`)
    }
  }

  const startViralRemake = async () => {
    setFeedback('')
    const result = await onStartViralRemake()
    if (!result) return
    if (result.taskId) {
      onReadyViralRemake(result.taskId)
      return
    }
    const missing = result.intake.missing_fields.map(intakeMissingLabel)
    setFeedback(missing.length
      ? `还不能进入创作：${missing.join('；')}。`
      : '当前需求仍需补充一项创作前信息。')
  }

  return <section className="kanon-conversation-workbench">
    <div className="kanon-conversation-grid compact">
      <div className="kanon-conversation-thread">
        <div className="kanon-message-list" ref={listRef}>
          {!messages.length ? <div className="kanon-conversation-empty-v2">
            <span><Bot size={19}/></span>
            <div>
              <small>从一句真实需求开始</small>
              <h3>不用整理成 Brief，也不用猜字段。</h3>
              <p>说出你已经确定的内容；缺的信息我只追问会改变创作结果的部分。</p>
            </div>
            <div className="kanon-starter-prompts" aria-label="需求示例">
              {starterPrompts.map(prompt => <button key={prompt} onClick={() => {
                setContent(prompt)
                textareaRef.current?.focus()
              }} type="button">{prompt}<ArrowUpRight size={13}/></button>)}
            </div>
          </div> : null}
          {messages.map(message => <ConversationMessage
            animate={streamingAssistantIds.has(message.id)}
            key={message.id}
            message={message}
            searchRun={conversationSearchByMessage.get(message.id)}
          />)}
          {pending ? <article className="kanon-message assistant thinking" aria-live="polite">
            <span>AI</span><div><small>Strategy 助手</small><p><LoaderCircle className="spin" size={14}/>{pendingPolicy?.web_search === 'allowed'
              ? pendingPolicy.reasoning_mode === 'deep'
                ? '正在联网检索；完成后会基于来源进行本轮深度分析并回答…'
                : '正在联网检索；完成后会基于返回来源生成本轮回答…'
              : pendingPolicy?.reasoning_mode === 'deep'
                ? '正在进行本轮深度分析，内部资料会单独标注…'
                : '正在区分事实、假设和仍需确认的问题…'}</p></div>
          </article> : null}
        </div>

        <div className="kanon-requirement-strip" aria-label="需求收敛状态">
          <div className="kanon-requirement-strip-copy">
            <span className={`kanon-requirement-state ${locked ? 'locked' : lens.coreReady ? 'ready' : ''}`}>
              {locked ? <CircleCheck size={14}/> : <Sparkles size={14}/>}
              {locked ? `需求 v${briefVersion?.version}` : `${lens.completedCore} / ${lens.totalCore} 项核心信息`}
            </span>
            <p>{locked
              ? strategyReady ? '需求已确认，可以进入策略。' : strategyBlocker?.reason || '需求已确认，完整策略仍需补充信息。'
              : lens.coreReady ? '核心信息已经够用，可以确认需求。' : `还差 ${lens.totalCore - lens.completedCore} 项核心信息，继续对话即可。`}</p>
          </div>
          <div className="kanon-requirement-strip-actions">
            <button className="kanon-lens-detail-link" onClick={onOpenBrief} type="button">查看 Brief <ArrowUpRight size={12}/></button>
            {locked && !strategyReady ? <button className="primary-button" onClick={onOpenFullStrategy} type="button">查看策略阻断</button>
              : locked && conversationCapabilities?.quick_viral_remake.available ? <button className="primary-button" disabled={Boolean(busy)} onClick={() => void startViralRemake()} type="button">
                  {busy === 'viral-remake' ? <LoaderCircle className="spin" size={14}/> : <Video size={14}/>}
                  {busy === 'viral-remake' ? '正在创建…' : '进入爆款裂变'}
                </button>
              : locked ? <button className="primary-button" onClick={onOpenFullStrategy} type="button">进入完整策略</button>
              : lens.coreReady ? <button aria-label="确认理解并锁定需求" className="primary-button" disabled={Boolean(busy) || pending} onClick={() => void onConfirmRequirement()} type="button">
                  {busy === 'confirm-requirement' ? <LoaderCircle className="spin" size={14}/> : <Check size={14}/>}
                  {busy === 'confirm-requirement' ? '确认中…' : '确认需求'}
                </button>
              : null}
          </div>
        </div>

        <form className="kanon-composer-v2" onSubmit={submit}>
          <div className="kanon-composer-bezel">
            <textarea
              aria-describedby="kanon-strategy-message-help"
              id="kanon-strategy-message"
              maxLength={4000}
              onChange={event => setContent(event.target.value)}
              onKeyDown={event => {
                if (event.key === 'Enter' && !event.shiftKey && !event.nativeEvent.isComposing) {
                  event.preventDefault()
                  event.currentTarget.form?.requestSubmit()
                }
              }}
              placeholder="例如：我们要给 FlowKit 做一条面向效率工具用户的短视频，希望提高试用转化…"
              ref={textareaRef}
              rows={3}
              value={content}
            />
            {attachedDocuments.length ? <div className="kanon-composer-attachments">
              {attachedDocuments.map(document => <span className={document.status} key={document.id}>
                {document.status === 'parse_queued' || document.status === 'parsing'
                  ? <LoaderCircle className="spin" size={12}/>
                  : <FileText size={12}/>}
                <b>{compactDocumentTitle(document)}</b>
                <small>{document.status === 'ready' ? `${document.chunk_count} 个片段` : document.status === 'parse_failed' ? '解析失败' : '解析中'}</small>
                <button aria-label={`移除附件 ${compactDocumentTitle(document)}`} onClick={() => setAttachedDocumentIds(current => current.filter(id => id !== document.id))} type="button"><X size={12}/></button>
              </span>)}
            </div> : null}
            {attachedMedia.length ? <div className="kanon-composer-attachments media">
              {attachedMedia.map(artifact => <span className={artifact.status} key={artifact.id}>
                {artifact.status === 'running'
                  ? <LoaderCircle className="spin" size={12}/>
                  : artifact.asset_kind === 'video' ? <Video size={12}/> : <ImageIcon size={12}/>}
                <b>{artifact.asset_kind === 'video' ? '短视频理解' : '图片理解'}</b>
                <small>{mediaArtifactStatus(artifact)}</small>
                <button aria-label="移除媒体附件" onClick={() => setAttachedMediaIds(current => current.filter(id => id !== artifact.id))} type="button"><X size={12}/></button>
              </span>)}
            </div> : null}
            {conversationCapabilities?.deep_reasoning.available || conversationCapabilities?.web_search.available
              ? <div className="kanon-composer-modes" aria-label="本轮 AI 能力">
                  {conversationCapabilities.deep_reasoning.available ? <button
                    aria-pressed={deepReasoning}
                    className={deepReasoning ? 'active' : ''}
                    disabled={Boolean(busy) || pending}
                    onClick={() => setDeepReasoning(current => !current)}
                    title={`仅本轮启用更强推理，发送成功后自动关闭；预计增加约 ${conversationCapabilities.deep_reasoning.estimated_wait_seconds ?? 30} 秒`}
                    type="button"
                  ><BrainCircuit size={13}/><span>深度思考</span><small>本轮</small></button> : null}
                  {conversationCapabilities.web_search.available ? <button
                    aria-pressed={webSearch}
                    className={webSearch ? 'active' : ''}
                    disabled={Boolean(busy) || pending}
                    onClick={() => setWebSearch(current => !current)}
                    title="只向外部服务发送当前问题，不发送附件正文；搜索完成后再生成本轮回答"
                    type="button"
                  ><Globe2 size={13}/><span>联网搜索</span><small>搜索后回答</small></button> : null}
                </div>
              : null}
            <footer>
              <div>
                {conversationCapabilities?.multimodal_input.available ? <>
                <label className={`kanon-attach-action ${busy === 'upload-document' ? 'busy' : ''}`} htmlFor="kanon-conversation-document">
                  {busy === 'upload-document' ? <LoaderCircle className="spin" size={14}/> : <Paperclip size={14}/>}
                  <span>{busy === 'upload-document' ? '正在上传' : '添加资料'}</span>
                </label>
                <input
                  accept=".pdf,.docx,.pptx,.md,.txt,.html,.htm"
                  disabled={Boolean(busy)}
                  id="kanon-conversation-document"
                  onChange={event => {
                    const file = event.target.files?.[0]
                    event.target.value = ''
                    void upload(file)
                  }}
                  type="file"
                />
                <label className={`kanon-attach-action ${busy === 'upload-media' ? 'busy' : ''}`} htmlFor="kanon-conversation-media">
                  {busy === 'upload-media' ? <LoaderCircle className="spin" size={14}/> : <ImageIcon size={14}/>}
                  <span>{busy === 'upload-media' ? '正在理解' : '图片 / 视频'}</span>
                </label>
                <input
                  accept="image/jpeg,image/png,video/mp4"
                  disabled={Boolean(busy)}
                  id="kanon-conversation-media"
                  onChange={event => {
                    const file = event.target.files?.[0]
                    event.target.value = ''
                    void uploadMedia(file)
                  }}
                  type="file"
                />
                </> : null}
                <span id="kanon-strategy-message-help">Enter 发送 · Shift + Enter 换行</span>
              </div>
              <div><small>{content.length} / 4000{content ? ' · 未发送内容仅在当前浏览器会话保留' : ''}</small><button aria-label="发送需求消息" disabled={Boolean(busy) || pending || (!content.trim() && !attachedDocuments.length && !attachedMedia.length) || !attachmentsReady} type="submit"><Send size={15}/></button></div>
            </footer>
          </div>
        </form>
        {feedback || notice ? <div className="kanon-conversation-feedback" role="status">{feedback || notice}</div> : null}
      </div>
    </div>
  </section>
}

function readConversationComposerDraft(key: string): ConversationComposerDraft | null {
  const value = readWorkspaceSessionValue<Partial<ConversationComposerDraft>>(key)
  if (!value || typeof value.content !== 'string' || typeof value.deepReasoning !== 'boolean' || typeof value.webSearch !== 'boolean') return null
  if (!Array.isArray(value.attachedDocumentIds) || !value.attachedDocumentIds.every(id => typeof id === 'string')) return null
  if (!Array.isArray(value.attachedMediaIds) || !value.attachedMediaIds.every(id => typeof id === 'string')) return null
  return {
    attachedDocumentIds: value.attachedDocumentIds,
    attachedMediaIds: value.attachedMediaIds,
    content: value.content,
    deepReasoning: value.deepReasoning,
    webSearch: value.webSearch,
  }
}

function ConversationMessage({
  animate,
  message,
  searchRun,
}: {
  animate: boolean
  message: Message
  searchRun?: ResearchRun
}) {
  return <article className={`kanon-message ${message.role}`}>
    <span>{message.role === 'user' ? '我' : message.role === 'assistant' ? 'AI' : '·'}</span>
    <div>
      <small>{message.role === 'user' ? '需求方' : message.role === 'assistant' ? 'Strategy 助手' : '系统事件'} · {formatTime(message.created_at)}</small>
      {message.requested_policy?.reasoning_mode === 'deep' || message.requested_policy?.web_search === 'allowed'
        ? <div className="kanon-message-policy" aria-label="本轮实际请求能力">
            {message.requested_policy.reasoning_mode === 'deep' ? <span><BrainCircuit size={11}/>深度思考</span> : null}
            {message.requested_policy.web_search === 'allowed' ? <span><Globe2 size={11}/>已请求联网搜索</span> : null}
          </div>
        : null}
      {message.content_blocks?.length
        ? <div className="kanon-message-blocks">{message.content_blocks.map((block, index) => <MessageBlock
            animate={animate && message.role === 'assistant'}
            block={block}
            key={`${block.type}-${index}`}
          />)}</div>
        : animate && message.role === 'assistant'
          ? <StreamingAssistantText text={message.content}/>
          : <p>{message.content}</p>}
      {searchRun ? <ConversationWebSearch run={searchRun}/> : null}
    </div>
  </article>
}

function StreamingAssistantText({ text }: { text: string }) {
  const characters = useMemo(() => Array.from(text), [text])
  const [visibleCount, setVisibleCount] = useState(0)

  useEffect(() => {
    setVisibleCount(0)
    if (!characters.length) return
    const chunkSize = Math.max(1, Math.ceil(characters.length / 90))
    const timer = window.setInterval(() => {
      setVisibleCount(current => {
        const next = Math.min(characters.length, current + chunkSize)
        if (next >= characters.length) window.clearInterval(timer)
        return next
      })
    }, 18)
    return () => window.clearInterval(timer)
  }, [characters])

  const complete = visibleCount >= characters.length
  return <p
    aria-label={complete ? undefined : 'Strategy 助手正在流式输出'}
    aria-live="polite"
    className={complete ? undefined : 'kanon-streaming-text'}
  >{characters.slice(0, visibleCount).join('')}</p>
}

function ConversationWebSearch({ run }: { run: ResearchRun }) {
	if (['queued', 'planning', 'searching', 'reading', 'cross_checking', 'drafting', 'auditing'].includes(run.status)) {
    return <div className="kanon-conversation-web-search running" role="status">
      <LoaderCircle className="spin" size={13}/><span><b>正在联网搜索</b><small>搜索完成后，Strategy 助手才会基于这些来源回答。</small></span>
    </div>
  }
	if (run.status !== 'completed' || !run.artifacts.length) {
    return <div className="kanon-conversation-web-search unavailable" role="status">
      <Globe2 size={13}/><span><b>本轮联网搜索未完成</b><small>没有生成无来源回答；可以重试，或关闭联网搜索后重新发送。</small></span>
    </div>
  }
  const artifact = run.artifacts[0]
  return <div className="kanon-conversation-web-search ready">
    <div><Globe2 size={13}/><span><b>本轮回答使用的联网证据</b><small>{artifact.title}</small></span></div>
    <p>{researchPreview(artifact.content)}</p>
    {artifact.sources.length ? <footer aria-label="联网搜索来源">
      {artifact.sources.slice(0, 3).map(source => <a href={source.url} key={source.id} rel="noreferrer" target="_blank">
        {source.title || source.domain}<ArrowUpRight size={10}/>
      </a>)}
    </footer> : null}
  </div>
}

function researchPreview(content: string) {
  const normalized = content.replace(/[#*_>`~-]+/g, ' ').replace(/\s+/g, ' ').trim()
  return normalized.length > 240 ? `${normalized.slice(0, 240)}…` : normalized
}

function MessageBlock({ animate = false, block }: { animate?: boolean; block: MessageContentBlock }) {
  if (block.type === 'text') return animate ? <StreamingAssistantText text={block.text}/> : <p>{block.text}</p>
  if (block.type === 'document_ref') {
    return <span className="kanon-message-ref" title={`Document ${block.document_id}`}><FileText size={13}/>资料附件 · 来源已锁定</span>
  }
  if (block.type === 'research_ref') {
    return <span className="kanon-message-ref research" title={`Research artifact ${block.research_artifact_id}`}><Globe2 size={13}/>联网证据 · 内容哈希已锁定</span>
  }
  return <span className="kanon-message-ref" title={`Asset ${block.asset_id} · v${block.asset_version}`}>
    {block.asset_kind === 'video' ? <Video size={13}/> : <ImageIcon size={13}/>}
    {block.asset_kind === 'video' ? '参考视频' : '参考图片'} · 已锁定版本 v{block.asset_version}
  </span>
}

function formatTime(value: string) {
  return new Intl.DateTimeFormat('zh-CN', { hour: '2-digit', minute: '2-digit' }).format(new Date(value))
}

function mediaArtifactStatus(artifact: MediaUnderstandingArtifact) {
  if (artifact.status === 'running') return '正在提取可定位证据'
  if (artifact.status === 'failed') return artifact.error_message || '理解失败'
  if (artifact.status === 'partial') {
    const qualifiers = [
      artifact.asset_kind === 'video' && artifact.keyframes.length ? `${artifact.keyframes.length} 个时间点` : '',
      artifact.warnings.includes('transcript_unavailable') ? '无音频转写' : '',
    ].filter(Boolean)
    const summary = artifact.summary || '仅获得部分证据，请人工复核'
    return qualifiers.length ? `${qualifiers.join(' · ')} · ${summary}` : summary
  }
  const evidenceCount = artifact.visible_text.length + artifact.observations.length
  if (artifact.asset_kind === 'video') return `${artifact.keyframes.length} 个时间点 · ${evidenceCount} 条直接证据`
  return `${evidenceCount} 条直接证据`
}
