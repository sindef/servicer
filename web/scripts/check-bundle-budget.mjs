import { readdirSync, statSync } from 'node:fs'
import { join } from 'node:path'

const distDir = join(process.cwd(), 'dist', 'assets')
const maxAssetBytes = Number.parseInt(process.env.BUNDLE_MAX_ASSET_BYTES || '450000', 10)
const maxEntryBytes = Number.parseInt(process.env.BUNDLE_MAX_ENTRY_BYTES || '200000', 10)

const files = readdirSync(distDir)
  .filter((name) => name.endsWith('.js'))
  .map((name) => ({
    name,
    size: statSync(join(distDir, name)).size
  }))
  .sort((a, b) => b.size - a.size)

if (files.length === 0) {
  throw new Error(`no JavaScript assets found in ${distDir}`)
}

const largest = files[0]
const entry = files.find((file) => file.name.startsWith('index-'))

if (!entry) {
  throw new Error('could not find entry bundle (index-*.js) in dist/assets')
}

if (largest.size > maxAssetBytes) {
  throw new Error(
    `bundle budget exceeded: largest asset ${largest.name} is ${largest.size} bytes (max ${maxAssetBytes})`
  )
}

if (entry.size > maxEntryBytes) {
  throw new Error(
    `bundle budget exceeded: entry asset ${entry.name} is ${entry.size} bytes (max ${maxEntryBytes})`
  )
}

console.log(
  `Bundle budget ok: largest=${largest.name} (${largest.size} bytes), entry=${entry.name} (${entry.size} bytes)`
)
