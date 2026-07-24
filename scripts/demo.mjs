#!/usr/bin/env node
// Records the README demo: assets/demo.gif plus the stills in assets/screens/.
//
// It builds hop and tools/demoserver, stands up a throwaway HOME with a seeded
// host database, starts the fake SSH server on loopback, and hands the whole thing
// to VHS (demo/hop.tape). Nothing real is involved: the hosts, the files, the shell
// output and the editor all come from the demo server, and hop runs against a HOME
// of its own with a generated key, so the user's own hosts, keys, agent and
// known_hosts are never touched.
//
//   node scripts/demo.mjs            # record everything
//   node scripts/demo.mjs --keep     # keep the throwaway HOME for inspection
//
// Requires: go, vhs (brew install vhs / go install github.com/charmbracelet/vhs).

import { spawn, spawnSync } from 'node:child_process'
import { existsSync, mkdirSync, rmSync, writeFileSync } from 'node:fs'
import { tmpdir } from 'node:os'
import path from 'node:path'
import process from 'node:process'

const repo = path.resolve(import.meta.dirname, '..')
const keep = process.argv.includes('--keep')

// A fixed port rather than an ephemeral one: it is baked into the seeded database,
// it is on screen in the details card, and a fixed one makes a failed run easy to
// diagnose ("who has 2222 open?").
const PORT = 2222
const ADDR = `127.0.0.1:${PORT}`

const demoHome = path.join(tmpdir(), 'hop-demo-home')
const binDir = path.join(tmpdir(), 'hop-demo-bin')

function run(cmd, args, opts = {}) {
  const r = spawnSync(cmd, args, { stdio: 'inherit', cwd: repo, ...opts })
  if (r.error) throw r.error
  if (r.status !== 0) {
    console.error(`\n${cmd} ${args.join(' ')} failed with status ${r.status}`)
    process.exit(r.status ?? 1)
  }
}

// configDir mirrors Go's os.UserConfigDir, which is where hop keeps hop.db and
// config.json — the demo HOME has to be laid out the same way or hop would open an
// empty database.
function configDir(home) {
  switch (process.platform) {
    case 'darwin':
      return path.join(home, 'Library', 'Application Support')
    case 'win32':
      return path.join(home, 'AppData', 'Roaming')
    default:
      return path.join(home, '.config')
  }
}

function requireTool(name) {
  const probe = process.platform === 'win32' ? 'where' : 'which'
  if (spawnSync(probe, [name], { stdio: 'ignore' }).status !== 0) {
    console.error(`demo: ${name} is not on PATH.`)
    if (name === 'vhs') console.error('       install it with: brew install vhs')
    process.exit(1)
  }
}

// ---- 1. tools and binaries ----

requireTool('go')
requireTool('vhs')

const exe = process.platform === 'win32' ? '.exe' : ''
const hopBin = path.join(binDir, `hop${exe}`)
const serverBin = path.join(binDir, `demoserver${exe}`)

console.log('demo: building hop and the demo server')
mkdirSync(binDir, { recursive: true })
// -tags hopdemo compiles in the keycast overlay (internal/tui/keycast.go): the strip
// of recent keypresses in the bottom-right of the recording. It is a recording aid,
// which is why it is a build tag rather than a flag — a released hop does not carry
// it at all.
run('go', ['build', '-tags', 'hopdemo', '-o', hopBin, '.'], { env: { ...process.env, CGO_ENABLED: '0' } })
run('go', ['build', '-o', serverBin, './tools/demoserver'], { env: { ...process.env, CGO_ENABLED: '0' } })

// ---- 2. a throwaway HOME ----

console.log(`demo: preparing ${demoHome}`)
rmSync(demoHome, { recursive: true, force: true })
const hopCfgDir = path.join(configDir(demoHome), 'hop')
for (const d of [hopCfgDir, path.join(demoHome, '.ssh'), path.join(demoHome, 'Downloads')]) {
  mkdirSync(d, { recursive: true })
}

// hop's own settings for the recording. The download directory is written as the
// literal "~/Downloads" so the settings card shows that rather than the absolute
// path of a temp directory, which is both ugly and pointless on screen.
writeFileSync(
  path.join(hopCfgDir, 'config.json'),
  `${JSON.stringify({ editor: '', downloadDir: '~/Downloads', accent: '212', openWith: '', vimKeys: false }, null, 2)}\n`,
)

// ---- 3. the fake host ----

console.log(`demo: starting the demo SSH server on ${ADDR}`)
const server = spawn(
  serverBin,
  [
    '-addr', ADDR,
    '-seed-db', path.join(hopCfgDir, 'hop.db'),
    '-known-hosts', path.join(demoHome, '.ssh', 'known_hosts'),
    '-client-key', path.join(demoHome, '.ssh', 'id_ed25519'),
  ],
  { cwd: repo, stdio: ['ignore', 'pipe', 'inherit'] },
)

let serverDown = false
server.on('exit', (code) => {
  serverDown = true
  if (code !== 0 && code !== null) console.error(`demo: the demo server exited with ${code}`)
})

// Wait for the server's own "listening" line rather than sleeping: a race here
// would show up as a red "connection refused" in the middle of the GIF.
await new Promise((resolve, reject) => {
  const timer = setTimeout(() => reject(new Error('the demo server did not come up within 15s')), 15_000)
  let buf = ''
  server.stdout.on('data', (chunk) => {
    buf += chunk
    process.stdout.write(`demo: ${chunk}`)
    if (buf.includes('listening on')) {
      clearTimeout(timer)
      resolve()
    }
  })
  server.on('exit', () => {
    clearTimeout(timer)
    reject(new Error('the demo server exited before it was listening'))
  })
}).catch((err) => {
  console.error(`demo: ${err.message}`)
  process.exit(1)
})

// ---- 4. record ----

// The recording's environment: hop's HOME is the throwaway one, the built binary is
// first on PATH, and SSH_AUTH_SOCK is cleared so hop authenticates with the
// generated key and never reaches the user's agent.
const recordEnv = { ...process.env, HOME: demoHome, PATH: `${binDir}${path.delimiter}${process.env.PATH}` }
delete recordEnv.SSH_AUTH_SOCK
delete recordEnv.XDG_CONFIG_HOME
if (process.platform === 'win32') recordEnv.USERPROFILE = demoHome

mkdirSync(path.join(repo, 'assets', 'screens'), { recursive: true })

console.log('demo: recording demo/hop.tape (this takes a minute)')
const vhs = spawnSync('vhs', [path.join('demo', 'hop.tape')], {
  stdio: 'inherit',
  cwd: repo,
  env: recordEnv,
})

// ---- 5. clean up ----

if (!serverDown) server.kill('SIGTERM')
if (!keep) rmSync(demoHome, { recursive: true, force: true })
else console.log(`demo: kept ${demoHome}`)

if (vhs.status !== 0) {
  console.error('demo: vhs failed')
  process.exit(vhs.status ?? 1)
}

const gif = path.join(repo, 'assets', 'demo.gif')
if (!existsSync(gif)) {
  console.error('demo: vhs reported success but assets/demo.gif is missing')
  process.exit(1)
}
console.log('demo: wrote assets/demo.gif and assets/screens/*.png')
