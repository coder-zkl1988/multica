"use client";

// Directly ported from Open Design v0.19.2
// apps/web/src/components/DesignSystemCreateHero.tsx (Apache-2.0).
// Only the icon import and Chinese product copy are adapted for Multica.

import { Sparkles } from "lucide-react";
import styles from "./open-design-system-create-hero.module.css";

const STEPS: { n: number; title: string; desc: string }[] = [
  { n: 1, title: "网站、仓库或 DESIGN.md", desc: "选择来源并固定当前版本" },
  { n: 2, title: "补充素材", desc: "图片、字体、参考链接均为可选" },
  { n: 3, title: "生成", desc: "先快速提取，Agent 再原地增强" },
];

const SWATCHES = ["#4f46e5", "#0ea5e9", "#14b8a6", "#f59e0b", "#f43f5e"];

export function OpenDesignSystemCreateHero({ stacked = false }: { stacked?: boolean } = {}) {
  return (
    <section className={`${styles.hero}${stacked ? ` ${styles.heroStacked}` : ""}`}>
      <div className={styles.copy}>
        <span className={styles.eyebrow}>
          <Sparkles size={14} />
          设计体系
        </span>
        <h1 className={styles.title}>几分钟，生成一套设计体系</h1>
        <p className={styles.lede}>
          把网站、代码仓库或 DESIGN.md，连同已经掌握的上下文，转成一套可以立即使用并持续增强的完整设计体系。
        </p>
        <div className={styles.meta}>
          <span className={styles.metaPill}><strong>3</strong> 步</span>
          <span className={styles.metaDot} aria-hidden />
          <span className={styles.metaPill}>先生成，再增强</span>
          <span className={styles.metaDot} aria-hidden />
          <span className={styles.metaPill}>DESIGN.md · Tokens · UI Kit · 预览</span>
        </div>
        <ol className={styles.steps}>
          {STEPS.map((step) => (
            <li key={step.n} className={styles.step}>
              <span className={styles.stepNo}>{step.n}</span>
              <span className={styles.stepText}>
                <strong>{step.title}</strong>
                <em>{step.desc}</em>
              </span>
            </li>
          ))}
        </ol>
      </div>
      <ShowcasePreview />
    </section>
  );
}

function ShowcasePreview() {
  return (
    <div className={styles.showcase} aria-hidden>
      <div className={styles.showcaseGlow} />
      <div className={styles.showcaseCard}>
        <div className={styles.showcaseHead}>
          <span className={styles.dot} />
          <span className={styles.dot} />
          <span className={styles.dot} />
          <span className={styles.showcaseTitle}>你的设计体系</span>
        </div>
        <div className={styles.section}>
          <span className={styles.sectionLabel}>色板</span>
          <div className={styles.swatches}>
            {SWATCHES.map((color) => <span key={color} className={styles.swatch} style={{ background: color }} />)}
          </div>
        </div>
        <div className={styles.section}>
          <span className={styles.sectionLabel}>字阶</span>
          <div className={styles.typeScale}>
            <span className={styles.typeLg}>Aa</span>
            <span className={styles.typeMd}>Aa</span>
            <span className={styles.typeSm}>Aa</span>
          </div>
        </div>
        <div className={styles.section}>
          <span className={styles.sectionLabel}>组件</span>
          <div className={styles.components}>
            <span className={styles.fauxBtn}>主要操作</span>
            <span className={styles.fauxBtnGhost}>次要操作</span>
            <div className={styles.fauxCard}>
              <span className={styles.fauxBar} />
              <span className={styles.fauxBarShort} />
            </div>
          </div>
        </div>
      </div>
    </div>
  );
}
