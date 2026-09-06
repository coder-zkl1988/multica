import * as React from 'react'
import { createHighlighterCore, type HighlighterCore } from 'shiki/core'
import { createJavaScriptRegexEngine } from 'shiki/engine/javascript'
import githubDark from 'shiki/themes/github-dark.mjs'
import githubLight from 'shiki/themes/github-light.mjs'
import bash from 'shiki/langs/bash.mjs'
import c from 'shiki/langs/c.mjs'
import cpp from 'shiki/langs/cpp.mjs'
import csharp from 'shiki/langs/csharp.mjs'
import css from 'shiki/langs/css.mjs'
import go from 'shiki/langs/go.mjs'
import html from 'shiki/langs/html.mjs'
import java from 'shiki/langs/java.mjs'
import javascript from 'shiki/langs/javascript.mjs'
import json from 'shiki/langs/json.mjs'
import jsx from 'shiki/langs/jsx.mjs'
import kotlin from 'shiki/langs/kotlin.mjs'
import markdown from 'shiki/langs/markdown.mjs'
import objectiveC from 'shiki/langs/objective-c.mjs'
import python from 'shiki/langs/python.mjs'
import ruby from 'shiki/langs/ruby.mjs'
import rust from 'shiki/langs/rust.mjs'
import sql from 'shiki/langs/sql.mjs'
import tsx from 'shiki/langs/tsx.mjs'
import typescript from 'shiki/langs/typescript.mjs'
import yaml from 'shiki/langs/yaml.mjs'
import { Copy, Check } from "lucide-react"
import { useTranslation } from "react-i18next"
import { Button } from "@multica/ui/components/ui/button"
import { Tooltip, TooltipTrigger, TooltipContent } from "@multica/ui/components/ui/tooltip"
import { cn } from '@multica/ui/lib/utils'
import { copyText } from '../lib/clipboard'
import {
  CODE_LIGATURE_CLASS,
  CODE_LIGATURE_DESCENDANT_CLASS,
} from '@multica/ui/lib/code-style'

export interface CodeBlockProps {
  code: string
  language?: string
  className?: string
  /**
   * Render mode affects code block styling:
   * - 'terminal': Minimal, keeps control chars visible
   * - 'minimal': Clean code, basic styling
   * - 'full': Rich styling with background, copy button, etc.
   */
  mode?: 'terminal' | 'minimal' | 'full'
}

const SUPPORTED_LANGUAGES = {
  shellscript: true,
  c: true,
  cpp: true,
  csharp: true,
  css: true,
  go: true,
  html: true,
  java: true,
  javascript: true,
  json: true,
  jsx: true,
  kotlin: true,
  markdown: true,
  'objective-c': true,
  python: true,
  ruby: true,
  rust: true,
  sql: true,
  tsx: true,
  typescript: true,
  yaml: true
} as const
type SupportedLanguage = keyof typeof SUPPORTED_LANGUAGES

// Map common aliases to the canonical names of the focused grammar bundle.
const LANGUAGE_ALIASES: Record<string, SupportedLanguage> = {
  bash: 'shellscript',
  sh: 'shellscript',
  shell: 'shellscript',
  zsh: 'shellscript',
  'c#': 'csharp',
  'c++': 'cpp',
  cs: 'csharp',
  js: 'javascript',
  mjs: 'javascript',
  cjs: 'javascript',
  ts: 'typescript',
  cts: 'typescript',
  mts: 'typescript',
  py: 'python',
  yml: 'yaml',
  md: 'markdown',
  rb: 'ruby',
  rs: 'rust',
  kt: 'kotlin',
  kts: 'kotlin',
  objc: 'objective-c'
}

let highlighterPromise: Promise<HighlighterCore> | undefined
function getHighlighter(): Promise<HighlighterCore> {
  highlighterPromise ??= createHighlighterCore({
    engine: createJavaScriptRegexEngine(),
    themes: [githubLight, githubDark],
    langs: [
      bash,
      c,
      cpp,
      csharp,
      css,
      go,
      html,
      java,
      javascript,
      json,
      jsx,
      kotlin,
      markdown,
      objectiveC,
      python,
      ruby,
      rust,
      sql,
      tsx,
      typescript,
      yaml
    ]
  })
  return highlighterPromise
}

function resolveLanguage(lang: string): SupportedLanguage | null {
  const normalized = LANGUAGE_ALIASES[lang] ?? lang
  return Object.prototype.hasOwnProperty.call(SUPPORTED_LANGUAGES, normalized)
    ? (normalized as SupportedLanguage)
    : null
}

// Simple LRU cache for highlighted code
const highlightCache = new Map<string, string>()
const CACHE_MAX_SIZE = 200

function getCacheKey(code: string, lang: string): string {
  return `${lang}:${code}`
}

/**
 * CodeBlock - Syntax highlighted code block using Shiki
 *
 * Uses Shiki dual themes with CSS variables for light/dark switching.
 * No JS-based dark mode detection needed — theme switching is handled
 * entirely via CSS (see globals.css for .shiki/.dark .shiki rules).
 *
 * @see https://shiki.style/guide/dual-themes
 */
export function CodeBlock({
  code,
  language = 'text',
  className,
  mode = 'full'
}: CodeBlockProps): React.JSX.Element {
  const { t } = useTranslation("ui")
  const [highlighted, setHighlighted] = React.useState<string | null>(null)
  const [isLoading, setIsLoading] = React.useState(true)
  const [copied, setCopied] = React.useState(false)

  const langLower = language.toLowerCase()
  const resolvedLang = resolveLanguage(langLower)
  const displayLanguage = String(LANGUAGE_ALIASES[langLower] ?? langLower)

  React.useEffect(() => {
    let cancelled = false

    async function highlight(): Promise<void> {
      if (!resolvedLang) {
        setHighlighted(null)
        setIsLoading(false)
        return
      }

      setIsLoading(true)
      const cacheKey = getCacheKey(code, resolvedLang)

      const cached = highlightCache.get(cacheKey)
      if (cached) {
        if (!cancelled) {
          setHighlighted(cached)
          setIsLoading(false)
        }
        return
      }

      try {
        // Dual themes: Shiki outputs CSS variables for both themes in one pass.
        // CSS handles switching via .dark selector (see globals.css).
        const html = (await getHighlighter()).codeToHtml(code, {
          lang: resolvedLang,
          themes: {
            light: 'github-light',
            dark: 'github-dark'
          },
          defaultColor: false
        })

        // Cache the result
        if (highlightCache.size >= CACHE_MAX_SIZE) {
          const firstKey = highlightCache.keys().next().value
          if (firstKey) highlightCache.delete(firstKey)
        }
        highlightCache.set(cacheKey, html)

        if (!cancelled) {
          setHighlighted(html)
          setIsLoading(false)
        }
      } catch (error) {
        // Fallback to plain text on error
        console.warn(`Shiki highlighting failed for language "${resolvedLang}":`, error)
        if (!cancelled) {
          setHighlighted(null)
          setIsLoading(false)
        }
      }
    }

    highlight()

    return () => {
      cancelled = true
    }
  }, [code, resolvedLang])

  const handleCopy = React.useCallback(async () => {
    if (await copyText(code)) {
      setCopied(true)
      setTimeout(() => setCopied(false), 2000)
    }
  }, [code])

  // Terminal mode: raw monospace with minimal styling
  if (mode === 'terminal') {
    return (
      <pre className={cn('font-mono text-body whitespace-pre-wrap', CODE_LIGATURE_CLASS, className)}>
        <code className={cn('font-mono', CODE_LIGATURE_CLASS)}>{code}</code>
      </pre>
    )
  }

  // Minimal mode: just syntax highlighting, no chrome
  if (mode === 'minimal') {
    if (isLoading || !highlighted) {
      return (
        <pre className={cn('font-mono text-body whitespace-pre-wrap', CODE_LIGATURE_CLASS, className)}>
          <code className={cn('font-mono', CODE_LIGATURE_CLASS)}>{code}</code>
        </pre>
      )
    }

    return (
      <div
        className={cn(
          'font-mono text-body [&_pre]:!bg-transparent [&_pre]:!p-0 [&_pre]:whitespace-pre-wrap [&_pre]:break-all [&_code]:!bg-transparent [&_code]:font-mono [&_pre]:font-mono',
          CODE_LIGATURE_CLASS,
          CODE_LIGATURE_DESCENDANT_CLASS,
          className
        )}
        dangerouslySetInnerHTML={{ __html: highlighted }}
      />
    )
  }

  // Full mode: rich styling with header and copy button
  return (
    <div
      className={cn(
        'relative group rounded-lg overflow-hidden border bg-muted/30 mb-4 last:mb-0',
        className
      )}
    >
      {/* Language label + copy button */}
      <div className="flex items-center justify-between px-3 py-1.5 bg-muted/50 border-b text-caption">
        <span className="text-muted-foreground font-medium uppercase tracking-wide">
          {displayLanguage !== 'text' ? displayLanguage : t(($) => $.plain_text)}
        </span>
        <Tooltip>
          <TooltipTrigger
            render={
              <Button
                variant="ghost"
                size="icon-xs"
                onClick={handleCopy}
                className="opacity-0 group-hover:opacity-100 transition-opacity text-muted-foreground hover:text-foreground"
                aria-label={t(($) => $.copy_code)}
              >
                {copied ? (
                  <Check className="size-3.5 text-success" />
                ) : (
                  <Copy className="size-3.5" />
                )}
              </Button>
            }
          />
          <TooltipContent>{t(($) => $.copy_code)}</TooltipContent>
        </Tooltip>
      </div>

      {/* Code content */}
      <div className="p-3 overflow-x-auto">
        {isLoading || !highlighted ? (
          <pre className={cn('font-mono text-body whitespace-pre-wrap break-all', CODE_LIGATURE_CLASS)}>
            <code className={cn('font-mono', CODE_LIGATURE_CLASS)}>{code}</code>
          </pre>
        ) : (
          <div
            className={cn(
              'font-mono text-body [&_pre]:!bg-transparent [&_pre]:!m-0 [&_pre]:!p-0 [&_pre]:whitespace-pre-wrap [&_pre]:break-all [&_code]:!bg-transparent [&_code]:font-mono [&_pre]:font-mono',
              CODE_LIGATURE_CLASS,
              CODE_LIGATURE_DESCENDANT_CLASS
            )}
            dangerouslySetInnerHTML={{ __html: highlighted }}
          />
        )}
      </div>
    </div>
  )
}

/**
 * InlineCode - Styled inline code span
 * Features: subtle background (3%), subtle border (5%), 75% opacity text
 */
export function InlineCode({
  children,
  className
}: {
  children: React.ReactNode
  className?: string
}): React.JSX.Element {
  return (
    <code
      className={cn(
        'px-1.5 py-0.5 rounded bg-foreground/[0.03] border border-foreground/[0.05] font-mono text-body text-foreground',
        CODE_LIGATURE_CLASS,
        className
      )}
    >
      {children}
    </code>
  )
}
