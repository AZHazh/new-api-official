/*
Copyright (C) 2023-2026 QuantumNous

This program is free software: you can redistribute it and/or modify
it under the terms of the GNU Affero General Public License as
published by the Free Software Foundation, either version 3 of the
License, or (at your option) any later version.

This program is distributed in the hope that it will be useful,
but WITHOUT ANY WARRANTY; without even the implied warranty of
MERCHANTABILITY or FITNESS FOR A PARTICULAR PURPOSE. See the
GNU Affero General Public License for more details.

You should have received a copy of the GNU Affero General Public License
along with this program. If not, see <https://www.gnu.org/licenses/>.

For commercial licensing, please contact support@quantumnous.com
*/
import {
  BookOpen,
  ChevronRight,
  FileText,
  Search,
  Sparkles,
} from 'lucide-react'
import { useEffect, useMemo, useState } from 'react'
import { useTranslation } from 'react-i18next'

import { PublicLayout } from '@/components/layout'
import { Markdown } from '@/components/ui/markdown'

import { TIANDOU_DOC_CONTENT, TIANDOU_DOC_TITLE } from './content'

type TocItem = {
  id: string
  level: 2 | 3
  text: string
}

type DocSection = {
  id: string
  text: string
  children: TocItem[]
}

function stripMarkdownText(text: string) {
  return text
    .replace(/!\[([^\]]*)\]\([^)]+\)/g, '$1')
    .replace(/\[([^\]]+)\]\([^)]+\)/g, '$1')
    .replace(/[`*_>#~]/g, '')
    .trim()
}

function createSlugifier() {
  const counts = new Map<string, number>()
  return (value: string) => {
    const base =
      stripMarkdownText(value)
        .toLowerCase()
        .replace(/[^\w\u4e00-\u9fff\s-]/g, '')
        .trim()
        .replace(/\s+/g, '-')
        .replace(/-+/g, '-') || 'section'
    const count = counts.get(base) || 0
    counts.set(base, count + 1)
    return count === 0 ? base : `${base}-${count}`
  }
}

function extractTocItems(markdown: string): TocItem[] {
  const slugify = createSlugifier()
  return markdown
    .split(/\r?\n/)
    .map((line) => line.match(/^(#{2,3})\s+(.+)$/))
    .filter((match): match is RegExpMatchArray => Boolean(match))
    .map((match) => ({
      id: slugify(match[2]),
      level: match[1].length as 2 | 3,
      text: stripMarkdownText(match[2]),
    }))
}

function extractLead(markdown: string) {
  const lines = markdown.split(/\r?\n/)
  const startIndex = lines.findIndex((line) => line.startsWith('# '))

  for (let index = startIndex + 1; index < lines.length; index += 1) {
    const line = lines[index].trim()
    if (!line || line.startsWith('>') || line.startsWith('#')) {
      continue
    }
    return stripMarkdownText(line)
  }

  return ''
}

function buildSections(tocItems: TocItem[]) {
  const sections: DocSection[] = []
  let currentSection: DocSection | null = null

  tocItems.forEach((item) => {
    if (item.level === 2) {
      currentSection = {
        id: item.id,
        text: item.text,
        children: [],
      }
      sections.push(currentSection)
      return
    }

    currentSection?.children.push(item)
  })

  return sections
}

function calculateReadingMinutes(markdown: string) {
  const words = stripMarkdownText(markdown).split(/\s+/).filter(Boolean).length

  return Math.max(3, Math.ceil(words / 220))
}

export function Docs() {
  const { t } = useTranslation()
  const tocItems = useMemo(() => extractTocItems(TIANDOU_DOC_CONTENT), [])
  const sections = useMemo(() => buildSections(tocItems), [tocItems])
  const lead = useMemo(() => extractLead(TIANDOU_DOC_CONTENT), [])
  const readingMinutes = useMemo(
    () => calculateReadingMinutes(TIANDOU_DOC_CONTENT),
    []
  )
  const imageCount = useMemo(
    () => (TIANDOU_DOC_CONTENT.match(/!\[/g) ?? []).length,
    []
  )
  const [activeId, setActiveId] = useState(tocItems[0]?.id ?? '')
  const [query, setQuery] = useState('')

  const filteredSections = useMemo(() => {
    const keyword = query.trim().toLowerCase()
    if (!keyword) {
      return sections
    }

    return sections
      .map((section) => {
        const sectionMatch = section.text.toLowerCase().includes(keyword)
        const children = section.children.filter((item) =>
          item.text.toLowerCase().includes(keyword)
        )

        if (!sectionMatch && children.length === 0) {
          return null
        }

        return {
          ...section,
          children: sectionMatch ? section.children : children,
        }
      })
      .filter((section): section is DocSection => Boolean(section))
  }, [query, sections])

  const quickLinks = useMemo(
    () =>
      sections.slice(0, 4).map((section, index) => ({
        id: section.id,
        text: section.text,
        eyebrow:
          [
            t('Prepare account'),
            t('Add balance'),
            t('Create API key'),
            t('Start setup'),
          ][index] ?? t('Continue reading'),
      })),
    [sections, t]
  )

  useEffect(() => {
    const slugify = createSlugifier()
    const headings = Array.from(
      document.querySelectorAll(
        '.tiandou-docs-content h2, .tiandou-docs-content h3'
      )
    )

    headings.forEach((heading) => {
      heading.id = slugify(heading.textContent || '')
      ;(heading as HTMLElement).style.scrollMarginTop = '112px'
    })

    const updateActiveHeading = () => {
      let currentId = tocItems[0]?.id ?? ''

      headings.forEach((heading) => {
        if ((heading as HTMLElement).getBoundingClientRect().top <= 160) {
          currentId = (heading as HTMLElement).id
        }
      })

      setActiveId((previous) => (previous === currentId ? previous : currentId))
    }

    updateActiveHeading()
    window.addEventListener('scroll', updateActiveHeading, { passive: true })

    return () => {
      window.removeEventListener('scroll', updateActiveHeading)
    }
  }, [tocItems])

  return (
    <PublicLayout>
      <div className='relative overflow-hidden'>
        <div className='pointer-events-none absolute inset-x-0 top-0 h-96 bg-[radial-gradient(circle_at_top_left,_rgba(59,130,246,0.16),_transparent_36%),radial-gradient(circle_at_top_right,_rgba(16,185,129,0.14),_transparent_34%)]' />
        <div className='mx-auto max-w-7xl px-4 py-8 md:py-10'>
          <section className='bg-background/92 relative overflow-hidden rounded-[32px] border p-6 shadow-sm md:p-8'>
            <div className='absolute inset-y-0 right-0 hidden w-1/2 bg-[linear-gradient(135deg,rgba(59,130,246,0.08),transparent_60%)] lg:block' />
            <div className='relative grid gap-8 lg:grid-cols-[minmax(0,1.6fr)_minmax(300px,0.9fr)] lg:items-end'>
              <div className='space-y-5'>
                <div className='bg-muted/80 text-foreground/80 inline-flex items-center gap-2 rounded-full border px-3 py-1 text-xs font-medium'>
                  <Sparkles
                    aria-hidden='true'
                    className='text-primary size-3.5'
                  />
                  {t('Documentation center')}
                </div>
                <div className='space-y-3'>
                  <h1 className='text-foreground max-w-3xl text-3xl font-semibold tracking-tight md:text-4xl'>
                    {TIANDOU_DOC_TITLE}
                  </h1>
                  <p className='text-muted-foreground max-w-3xl text-sm leading-6 md:text-base'>
                    {lead}
                  </p>
                </div>
                <div className='text-muted-foreground flex flex-wrap gap-3 text-sm'>
                  <div className='bg-background rounded-full border px-3 py-1.5'>
                    {t('{{count}} main sections', {
                      count: sections.length,
                    })}
                  </div>
                  <div className='bg-background rounded-full border px-3 py-1.5'>
                    {t('{{count}} detailed steps', {
                      count: tocItems.filter((item) => item.level === 3).length,
                    })}
                  </div>
                  <div className='bg-background rounded-full border px-3 py-1.5'>
                    {t('About {{count}} minutes to read', {
                      count: readingMinutes,
                    })}
                  </div>
                  <div className='bg-background rounded-full border px-3 py-1.5'>
                    {t('{{count}} illustrations', { count: imageCount })}
                  </div>
                </div>
              </div>
              <div className='grid gap-3 sm:grid-cols-2 lg:grid-cols-1'>
                {quickLinks.map((item) => (
                  <a
                    key={item.id}
                    className='group bg-background/90 hover:border-primary/40 hover:bg-primary/5 rounded-2xl border p-4 transition-colors'
                    href={`#${item.id}`}
                  >
                    <div className='text-muted-foreground mb-1 text-xs font-medium tracking-[0.14em] uppercase'>
                      {item.eyebrow}
                    </div>
                    <div className='flex items-center justify-between gap-3'>
                      <span className='text-foreground text-sm font-medium'>
                        {item.text}
                      </span>
                      <ChevronRight
                        aria-hidden='true'
                        className='text-muted-foreground group-hover:text-primary size-4 transition-transform group-hover:translate-x-0.5'
                      />
                    </div>
                  </a>
                ))}
              </div>
            </div>
          </section>

          <div className='mt-6 grid gap-6 xl:grid-cols-[280px_minmax(0,1fr)_240px]'>
            <aside className='bg-background/95 h-fit rounded-[28px] border p-4 shadow-sm xl:sticky xl:top-24'>
              <div className='text-foreground mb-4 flex items-center gap-2 text-sm font-semibold'>
                <BookOpen aria-hidden='true' className='text-primary size-4' />
                {t('Quick navigation')}
              </div>
              <label className='bg-muted/50 text-muted-foreground mb-4 flex items-center gap-2 rounded-2xl border px-3 py-2 text-sm'>
                <Search aria-hidden='true' className='size-4 shrink-0' />
                <input
                  aria-label={t('Search documentation sections')}
                  className='text-foreground placeholder:text-muted-foreground w-full bg-transparent outline-none'
                  value={query}
                  onChange={(event) => setQuery(event.target.value)}
                  placeholder={t('Search sections')}
                />
              </label>
              <nav aria-label={t('Documentation navigation')}>
                <ul className='space-y-4'>
                  {filteredSections.map((section) => (
                    <li key={section.id}>
                      <a
                        className={[
                          'block rounded-2xl px-3 py-2 text-sm font-medium transition-colors',
                          activeId === section.id
                            ? 'bg-primary/12 text-primary'
                            : 'text-foreground hover:bg-muted',
                        ].join(' ')}
                        href={`#${section.id}`}
                      >
                        {section.text}
                      </a>
                      {section.children.length > 0 && (
                        <ul className='border-border/70 mt-2 space-y-1 border-s ps-3'>
                          {section.children.map((item) => (
                            <li key={item.id}>
                              <a
                                className={[
                                  'block rounded-xl px-3 py-1.5 text-sm transition-colors',
                                  activeId === item.id
                                    ? 'bg-primary/10 font-medium text-primary'
                                    : 'text-muted-foreground hover:bg-muted hover:text-foreground',
                                ].join(' ')}
                                href={`#${item.id}`}
                              >
                                {item.text}
                              </a>
                            </li>
                          ))}
                        </ul>
                      )}
                    </li>
                  ))}
                </ul>
              </nav>
            </aside>

            <div className='bg-background/90 min-w-0 rounded-[28px] border shadow-sm'>
              <div className='flex items-center justify-between gap-3 border-b px-5 py-4 md:px-8'>
                <div>
                  <div className='text-foreground text-sm font-semibold'>
                    {t('Quick start')}
                  </div>
                  <div className='text-muted-foreground text-sm'>
                    {t(
                      'Follow the guide to register, add balance, create an API key, and connect Codex.'
                    )}
                  </div>
                </div>
                <div className='bg-muted/60 text-muted-foreground hidden rounded-full border px-3 py-1.5 text-xs md:block'>
                  {t(
                    'Local environment verification is usually the final step.'
                  )}
                </div>
              </div>
              <div className='px-5 py-6 md:px-8 md:py-8'>
                <Markdown className='tiandou-docs-content prose prose-neutral prose-headings:scroll-mt-28 prose-headings:font-semibold prose-h1:text-4xl prose-h1:tracking-tight prose-h2:mt-12 prose-h2:border-b prose-h2:pb-3 prose-h2:text-2xl prose-h3:mt-8 prose-h3:text-xl prose-p:leading-7 prose-li:leading-7 prose-img:rounded-2xl prose-img:border prose-img:shadow-sm dark:prose-invert max-w-none'>
                  {TIANDOU_DOC_CONTENT}
                </Markdown>
              </div>
            </div>

            <aside className='hidden xl:block'>
              <div className='sticky top-24 space-y-4'>
                <div className='bg-background/95 rounded-[28px] border p-4 shadow-sm'>
                  <div className='text-foreground mb-3 flex items-center gap-2 text-sm font-semibold'>
                    <FileText
                      aria-hidden='true'
                      className='text-primary size-4'
                    />
                    {t('On this page')}
                  </div>
                  <nav aria-label={t('Page contents')}>
                    <ul className='space-y-1'>
                      {tocItems.map((item) => (
                        <li key={item.id}>
                          <a
                            className={[
                              'block rounded-xl px-3 py-2 text-sm transition-colors',
                              item.level === 3 ? 'ms-3 text-[13px]' : '',
                              activeId === item.id
                                ? 'bg-primary/12 font-medium text-primary'
                                : 'text-muted-foreground hover:bg-muted hover:text-foreground',
                            ].join(' ')}
                            href={`#${item.id}`}
                          >
                            {item.text}
                          </a>
                        </li>
                      ))}
                    </ul>
                  </nav>
                </div>

                <div className='bg-background/95 rounded-[28px] border p-4 shadow-sm'>
                  <div className='text-foreground mb-2 text-sm font-semibold'>
                    {t('Reading tips')}
                  </div>
                  <div className='text-muted-foreground space-y-3 text-sm leading-6'>
                    <p>
                      {t(
                        'Finish account setup, payment, and API key creation before configuring the local CLI.'
                      )}
                    </p>
                    <p>
                      {t(
                        'If a command fails, first check Node.js and your config.toml file.'
                      )}
                    </p>
                  </div>
                </div>
              </div>
            </aside>
          </div>
        </div>
      </div>
    </PublicLayout>
  )
}
