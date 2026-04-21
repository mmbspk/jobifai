import { scoreColor, scoreBg } from '../lib'

interface Props {
  score: number
}

export function ScorePill({ score }: Props) {
  return (
    <span
      className="inline-flex items-center rounded-full px-2 py-0.5 text-xs font-semibold"
      style={{ background: scoreBg(score), color: scoreColor(score) }}
    >
      {score}/10
    </span>
  )
}
