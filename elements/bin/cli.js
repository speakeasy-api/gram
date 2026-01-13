#!/usr/bin/env node

import { execSync } from 'node:child_process'
import { existsSync } from 'node:fs'
import { resolve } from 'node:path'

// Colors
const c = {
  reset: '\x1b[0m',
  bold: '\x1b[1m',
  dim: '\x1b[2m',
  cyan: '\x1b[36m',
  green: '\x1b[32m',
  yellow: '\x1b[33m',
  red: '\x1b[31m',
  magenta: '\x1b[35m',
  blue: '\x1b[34m',
  bgCyan: '\x1b[46m',
  bgBlue: '\x1b[44m',
  white: '\x1b[37m',
}

const PEER_DEPS = [
  'react',
  'react-dom',
  'motion',
  'remark-gfm',
  'zustand',
  'vega',
  'shiki',
]

const PACKAGE_NAME = '@gram-ai/elements'

function detectPackageManager() {
  const cwd = process.cwd()

  // Check for lockfiles
  if (
    existsSync(resolve(cwd, 'bun.lockb')) ||
    existsSync(resolve(cwd, 'bun.lock'))
  ) {
    return 'bun'
  }
  if (existsSync(resolve(cwd, 'pnpm-lock.yaml'))) {
    return 'pnpm'
  }
  if (existsSync(resolve(cwd, 'yarn.lock'))) {
    return 'yarn'
  }
  if (existsSync(resolve(cwd, 'package-lock.json'))) {
    return 'npm'
  }

  // Check for npm_config_user_agent (set when running via npx/pnpm dlx/etc)
  const userAgent = process.env.npm_config_user_agent || ''
  if (userAgent.includes('bun')) return 'bun'
  if (userAgent.includes('pnpm')) return 'pnpm'
  if (userAgent.includes('yarn')) return 'yarn'

  // Default to npm
  return 'npm'
}

function getInstallCommand(pm, packages) {
  const pkgString = packages.join(' ')
  switch (pm) {
    case 'bun':
      return `bun add ${pkgString}`
    case 'pnpm':
      return `pnpm add ${pkgString}`
    case 'yarn':
      return `yarn add ${pkgString}`
    case 'npm':
    default:
      return `npm install ${pkgString}`
  }
}

function run(command) {
  console.log(`\n  ${c.dim}$${c.reset} ${c.cyan}${command}${c.reset}\n`)
  try {
    execSync(command, { stdio: 'inherit', cwd: process.cwd() })
    return true
  } catch {
    return false
  }
}

function printUsage() {
  console.log(`
${c.bold}Usage:${c.reset} npx ${c.cyan}${PACKAGE_NAME}${c.reset} ${c.dim}<command>${c.reset}

${c.bold}Commands:${c.reset}
  ${c.green}install${c.reset}    Install ${PACKAGE_NAME} and its peer dependencies
  ${c.green}help${c.reset}       Show this help message

${c.bold}Examples:${c.reset}
  ${c.dim}$${c.reset} npx ${c.cyan}${PACKAGE_NAME}${c.reset} install
`)
}

function install() {
  const pm = detectPackageManager()

  console.log(`
${c.bold}⚡ Installing Gram Elements${c.reset}

${c.dim}Package manager:${c.reset} ${c.cyan}${pm}${c.reset}
`)

  // Install everything in one command
  console.log(`${c.yellow}○${c.reset} Installing ${c.cyan}@gram-ai/elements${c.reset} and peer dependencies...`)
  const allPackages = [...PEER_DEPS, PACKAGE_NAME]
  const cmd = getInstallCommand(pm, allPackages)
  if (!run(cmd)) {
    console.error(`\n${c.red}✖${c.reset} Failed to install packages`)
    process.exit(1)
  }

  console.log(`
${c.green}${c.bold}✔ Installation complete!${c.reset}
`)
}

// Main
const command = process.argv[2]

switch (command) {
  case 'install':
  case 'i':
    install()
    break
  case 'help':
  case '--help':
  case '-h':
  case undefined:
    printUsage()
    break
  default:
    console.error(`Unknown command: ${command}`)
    printUsage()
    process.exit(1)
}
