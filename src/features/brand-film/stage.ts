export const brandFilmStageIds = ['brief', 'concept', 'storyboard', 'generation', 'audio'] as const

export type BrandFilmStageId = typeof brandFilmStageIds[number]
export type BrandFilmStageState = {
  id: BrandFilmStageId
  order: number
  label: string
  description: string
  accessible: boolean
  complete: boolean
  lockedReason?: string
}

export type BrandFilmStageFacts = {
  briefConfirmed: boolean
  conceptSelected: boolean
  planConfirmed: boolean
  visualPreviewReady: boolean
  audioPreviewReady: boolean
}

const definitions: Array<Pick<BrandFilmStageState, 'id' | 'label' | 'description'>> = [
  { id: 'brief', label: 'Brief 确认', description: '核对事实、卖点、限制与参考素材' },
  { id: 'concept', label: '创意方向', description: '比较并选择差异化叙事方向' },
  { id: 'storyboard', label: '剧本分镜', description: '编辑剧本、声音意图与镜头执行' },
  { id: 'generation', label: '视频生成', description: '逐镜头生成、反馈与锁定' },
  { id: 'audio', label: '声音导演', description: '编排音乐、环境氛围与镜头音效' },
]

export function deriveBrandFilmStages(facts: BrandFilmStageFacts): BrandFilmStageState[] {
  const accessibility: Record<BrandFilmStageId, { accessible: boolean; reason?: string }> = {
    brief: { accessible: true },
    concept: { accessible: facts.briefConfirmed, reason: '请先确认 Brief 与参考素材' },
    storyboard: { accessible: facts.conceptSelected, reason: '请先选择一个创意方向' },
    generation: { accessible: facts.planConfirmed, reason: '请先保存并确认剧本与分镜' },
    audio: { accessible: facts.visualPreviewReady, reason: '请先锁定镜头并合成视觉预览' },
  }
  const completion: Record<BrandFilmStageId, boolean> = {
    brief: facts.briefConfirmed,
    concept: facts.conceptSelected,
    storyboard: facts.planConfirmed,
    generation: facts.visualPreviewReady,
    audio: facts.audioPreviewReady,
  }

  return definitions.map((definition, index) => ({
    ...definition,
    order: index + 1,
    accessible: accessibility[definition.id].accessible,
    complete: completion[definition.id],
    lockedReason: accessibility[definition.id].accessible ? undefined : accessibility[definition.id].reason,
  }))
}

export function isBrandFilmStageId(value: string | null | undefined): value is BrandFilmStageId {
  return Boolean(value && brandFilmStageIds.includes(value as BrandFilmStageId))
}

export function resolveBrandFilmStage(requested: string | null | undefined, stages: BrandFilmStageState[]): BrandFilmStageId {
  if (isBrandFilmStageId(requested) && stages.some(stage => stage.id === requested && stage.accessible)) return requested
  for (let index = stages.length - 1; index >= 0; index -= 1) {
    if (stages[index]?.accessible) return stages[index].id
  }
  return 'brief'
}
