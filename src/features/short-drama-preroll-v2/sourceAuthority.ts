type AssetVersionRef = { asset_id: string; version: number }
type ProjectVideo = { id: string; version: number; kind: string }

type PersistedVideoMetadata = {
  projectId: string
  width?: number
  height?: number
  durationSeconds?: number
  createdAt?: string
  contentUrl: string
}

export const sourceUnavailableMessage = '源视频素材已失效或不属于当前项目，请重新上传后继续。'

export function findAuthoritativeVideo<T extends ProjectVideo>(videos: T[], ref: AssetVersionRef): T | null {
  return videos.find(video => video.kind === 'video' && video.id === ref.asset_id && video.version === ref.version) ?? null
}

export function requireAuthoritativeVideo<T extends ProjectVideo>(videos: T[], ref: AssetVersionRef): T {
  const video = findAuthoritativeVideo(videos, ref)
  if (!video) throw new Error('后端尚未确认该视频素材，请重新上传后继续。')
  return video
}

export function restorePersistedVideo(ref: AssetVersionRef, metadata: PersistedVideoMetadata) {
  return {
    id: ref.asset_id,
    projectId: metadata.projectId,
    version: ref.version,
    kind: 'video' as const,
    sourceType: 'imported' as const,
    mimeType: 'video/mp4',
    sizeBytes: 0,
    durationSeconds: metadata.durationSeconds,
    width: metadata.width,
    height: metadata.height,
    createdAt: metadata.createdAt ?? '',
    contentUrl: metadata.contentUrl,
    useAllowed: true,
  }
}
