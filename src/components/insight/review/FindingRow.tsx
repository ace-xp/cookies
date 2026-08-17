import { RotateCcw, Trash2 } from 'lucide-react'
import type { ApiReportFinding } from '../../../data/api'
import { ThresholdStamp, VerdictBadge } from '../shared'

/**
 * 复盘里的一条发现。
 *
 * 左边那个点区分来源：● 是人在分析页记的，○ 是系统提交时补的。这个区分不是装饰
 * ——复盘会上「我当时特意留了这条」和「系统扫出来的」份量完全不同，混在一起显示，
 * 人自己也想不起来哪条是哪条。
 */
export function FindingRow({ finding, index, editable, onDrop }: {
  finding: ApiReportFinding
  index: number
  editable: boolean
  onDrop: (index: number, dropped: boolean) => void
}) {
  const pinned = finding.origin === 'pinned'
  return <li className={finding.dropped ? 'finding-row dropped' : 'finding-row'}>
    <span className={pinned ? 'finding-origin pinned' : 'finding-origin'}
      title={pinned ? '我在分析页记的' : '提交时系统补的'}>
      {pinned ? '●' : '○'}
    </span>
    <div className="finding-body">
      <p>{finding.text}</p>
      {/* 判定是可选的：口径警告、下一轮建议这类自由文本没有档位，硬给一个
          「算不出来」会让人以为那句话本身不可靠。 */}
      {finding.verdict ? <VerdictBadge judgement={{
        confidence: finding.confidence ?? 'low_sample',
        verdict: finding.verdict,
        verdict_label: finding.verdict_label ?? '',
        upgrade: finding.upgrade,
        note: finding.note ?? '',
      }}/> : null}
      {/* 一份复盘里的发现可能来自不同时间的分析，所以标在每一条上而不是报告顶部。 */}
      {finding.verdict ? <ThresholdStamp version={finding.threshold_version}/> : null}
      {/* 定格的日子。上面那句总的披露说了「档位不重算」，这里回答「那是哪天的判断」
          ——两条记在同一份复盘里的发现可能差好几天，只给一句总的说明还是对不上号。 */}
      {finding.verdict && finding.pinned_at
        ? <small className="finding-note">按 {formatDay(finding.pinned_at)} 那天的数据判的</small>
        : null}
      {finding.note ? <small className="finding-note">{finding.note}</small> : null}
    </div>
    {editable ? <button type="button" className="text-button"
      onClick={() => onDrop(index, !finding.dropped)}
      title={finding.dropped ? '恢复这一条' : '这一条不要'}>
      {finding.dropped ? <RotateCcw size={14}/> : <Trash2 size={14}/>}
    </button> : null}
  </li>
}

// pinned_at 是带时区的时间戳，这里只要日子。判定的粒度本来就是天，
// 精确到秒会让人以为「这一档是那一秒算的」，而它其实是那一天的数据算的。
function formatDay(value: string): string {
  const date = new Date(value)
  return Number.isNaN(date.valueOf())
    ? value
    : `${date.getFullYear()}/${date.getMonth() + 1}/${date.getDate()}`
}
