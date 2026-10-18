import { spawnSync } from 'node:child_process'
import { resolve } from 'node:path'

// Invoke with node --env-file=.env; only the LLM client receives synthetic fixtures.
const result = spawnSync(
  'go',
  ['test', '-count=1', '-timeout', '6m', '-run', '^TestLiveAcceptance$', '-v', './internal/llm'],
  {
    env: {
      ...process.env,
      LLM_EVAL_LIVE: '1',
      LLM_EVAL_REPORT: resolve(process.env.LLM_EVAL_REPORT || '.cache/llm-eval.json'),
    },
    stdio: 'inherit',
    windowsHide: true,
    timeout: 7 * 60 * 1000,
  },
)
if (result.error) console.error(result.error.message)
process.exitCode = result.status ?? 1
