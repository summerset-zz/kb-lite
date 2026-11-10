<script setup lang="ts">
/*
 * 帮助页（`#help`，导航第二个页签）。
 *
 * 为什么不用 markdown 渲染：这份内容全是静态的、由我们自己写，只有一页。
 * 引 markdown-it 是 40 KB 起步，还要配 sanitize；而一旦有了 `v-html` 这条通道，
 * 迟早会有人拿它去渲染用户内容，那是 AGENTS.md 明令禁止的事。
 * 所以这里是手写标记：多花几行标签，换掉一个依赖和一类风险。
 *
 * 文案规则见 docs/11-writing-style.md 与 docs/06 第八节：不用角括号、不用破折号、
 * 对读者说“你”、一句话一个意思。示例代码放常量里用插值输出，免得模板压缩空白把缩进吃掉。
 */
import { s } from '../strings'
import AppIcon from '../components/AppIcon.vue'

const releaseURL = 'https://github.com/summerset-zz/kb-lite/releases'
const issuesURL = 'https://github.com/summerset-zz/kb-lite/issues'

// 文件头示例。第一行与最后一行都是三个连字符，中间是“键: 值”。
const frontmatterSample = `---
title: 上线与回滚
summary: 每次发布的完整流程，以及出问题时怎么退回上一个版本。
tags: [部署, deploy, 回滚, rollback]
---`

// 正文示例：导言、普通节、空节里套子节。
const bodySample = `# 上线

这里的导言会变成摘要，最多取两百字，显示在每条搜索结果的开头。

## 发布前

这一节的正文。子标题里的内容不算进来，字数也按这个算。

### 检查清单

- 打 tag 之前确认流水线是绿的。

## 回滚

### 快速回滚

回滚到上一个 tag。`
</script>

<template>
  <div class="mx-auto max-w-3xl px-4 sm:px-6 py-6 help">
    <h1 class="font-medium text-lg">{{ s.help.title }}</h1>
    <p class="muted text-sm mt-1">{{ s.help.lead }}</p>

    <h2>创建你的第一个知识库</h2>
    <p>打开知识库页，点右上角的“添加知识库”。还没有库的时候，页面中间的按钮也是它。</p>
    <ol>
      <li>在弹窗里选一个文件夹。可以一层层点，也可以把路径粘到最上面那一行，按回车跳过去。</li>
      <li>给知识库起个名字。AI 能看到这个名字，取一个你以后也认得的。</li>
      <li>点“添加这个知识库”。建好之后会立刻开始整理，进度显示在那一行上。</li>
    </ol>
    <p>
      目录里的 md、markdown、txt 会被收录，其它文件不动。所以最好是一个只放资料的文件夹。
      空文件夹也可以，以后往里放就行。
    </p>
    <p class="note">
      文件夹在你自己用户目录里的时候，同一个电脑上的其他用户读不到它，内容变了也更新不了。
      想让几个人共用，把资料放到 D:\ 这类公共位置。
    </p>

    <h2>安装 SKILL</h2>
    <p>
      在知识库页往下滚，有一块“让 AI 用起来”，分两组。上面那份教 AI 怎么查，
      下面那份教 AI 怎么把 docx、pdf 整理成能进知识库的格式。每组有两个目标目录，
      按你用的 AI 工具装对应的那个。
    </p>
    <ul>
      <li>没装过时按钮写“安装”，点一下就写进那个目录。</li>
      <li>装过之后，那一行会显示是什么时候装的。想再写一遍就点“重新安装”。</li>
      <li>
        显示“SKILL 有更新”的时候，说明装好的那份和现在该装的不一样了。比如你改过端口或访问密码，
        或者程序升级后说明书本身变了。点那一行右边的按钮更新一下。
      </li>
    </ul>
    <p>
      装完要重开一次 AI 工具，它只在启动的时候读那个目录。Claude Code 会自己发现目录里的改动，
      其它工具不一定。
    </p>
    <p class="muted">
      不想安装也可以。每组下面有一句“怎么跟 AI 说”，把它和说明书地址一起贴给 AI，效果一样。
    </p>

    <h2>向知识库添加内容</h2>

    <h3>让其他 AI 代劳</h3>
    <p>
      手上有一堆 docx、pdf 或者网页，想让它们进知识库。kb-lite 不解析这些格式，
      但整理 SKILL 写清了应该转成什么样。
    </p>
    <p>
      用法是把它发给另一个 AI，告诉它哪些文件要进哪个知识库。它会先给你一份清单，
      写完之后用检查接口确认每一篇都能被收录。它需要服务地址与访问密码，说明书里都写了。
    </p>
    <p class="note">
      写完不会自动更新。回到知识库页，点那一行右边的“更新”，等整理完，AI 才查得到新内容。
      整理完还可以点“查看入库日志”，看哪些文件没进去、为什么。
    </p>

    <h3>自己写</h3>
    <p>这一节假定你会写 Markdown（用符号排版的纯文本格式）。</p>
    <p>文件开头是 frontmatter（文件头设置），用两行 <code>---</code> 包住：</p>
    <pre>{{ frontmatterSample }}</pre>
    <ul>
      <li>第一行必须是 <code>---</code>，单独占一行。</li>
      <li>每项写成 <code>键: 值</code> 的形式，冒号后面留一个空格。</li>
      <li>值里带冒号的时候，用引号包起来。比如 <code>summary: "在 a 与 b 之间切换"</code>。</li>
      <li><code>tags</code> 写成方括号加逗号，也可以写成逗号分隔的一串。</li>
      <li>结尾再写一行 <code>---</code>，后面才是正文。</li>
    </ul>
    <p>键名只有三个：<code>title</code>、<code>summary</code>、<code>tags</code>。其它键会被忽略。</p>

    <p>正文里的 <code>#</code> 标题划分章节。一个标题到下一个标题之间，是这一节自己的正文。</p>
    <pre>{{ bodySample }}</pre>
    <p>这段示例里有几点值得说明：</p>
    <ul>
      <li>第一个标题之前的内容是导言。它会被截成摘要，所以第一段要能独立说明这篇讲什么。</li>
      <li>每个标题只算属于自己的那部分正文，子标题里的不算。</li>
      <li>
        标题下面没有正文、内容全在子标题里，是允许的。<code>## 回滚</code> 就是这样，
        目录里会显示成一个空节。
      </li>
      <li>标题只按出现的顺序编号，从上往下数，不按层级。不要自己写 1.2.3 这样的编号。</li>
      <li>一节超过一千五百字，读取时会被截断。</li>
      <li>代码块要用三个反引号闭合，没闭合会把后面的标题全吞进去。</li>
    </ul>
    <p class="muted">
      想边写边确认？在知识库那一行点“更新”，再用“查看入库日志”看结果。
    </p>

    <h2>版本更新</h2>
    <ol>
      <li>
        到
        <a :href="releaseURL" target="_blank" rel="noopener">Releases 页面</a>
        下载新版本。
      </li>
      <li>解压到一个固定的文件夹。别留在“下载”里，那里随时会被清掉。</li>
      <li>
        用新的程序替换旧的，最好是同一个文件夹、同一个文件名。配置和整理结果不在程序目录里，
        换掉不影响它们。
      </li>
      <li>打开界面，看底部的版本号，确认换成了新的。</li>
      <li>
        如果换了文件夹，去看设置页的开机自启和创建桌面快捷方式。它们记着程序的位置，
        指向另一份时会直接告诉你，旁边有一个按钮改成指向现在这份。
      </li>
      <li>升级之后说明书可能也变了。回知识库页看那两行有没有“SKILL 有更新”，有就点一下。</li>
    </ol>

    <h2>常见问题</h2>
    <p class="muted">
      还没写。遇到问题先看知识库那一行的“查看入库日志”。还是不行就到
      <a :href="issuesURL" target="_blank" rel="noopener">反馈问题</a>
      里说一声，把当时的情况和日志一起带上。
    </p>

    <p class="flex items-center gap-2 text-sm mt-8">
      <AppIcon name="check" :style="{ color: 'var(--accent)' }" />
      {{ s.help.done }}
    </p>
  </div>
</template>

<style scoped>
/* preflight 把列表符与标题字号都抹平了，这里按“读一篇说明”的密度写回来。 */
.help h2 {
  margin-top: 2rem;
  font-size: 16px;
  font-weight: 600;
}
.help h3 {
  margin-top: 1.25rem;
  font-size: 14px;
  font-weight: 600;
}
.help p {
  margin-top: 0.6rem;
  font-size: 14px;
  line-height: 1.75;
}
.help ul,
.help ol {
  margin-top: 0.6rem;
  padding-left: 1.2rem;
}
.help ul {
  list-style: disc;
}
.help ol {
  list-style: decimal;
}
.help li {
  margin-top: 0.3rem;
  font-size: 14px;
  line-height: 1.75;
}
.help code {
  padding: 1px 4px;
  border-radius: 4px;
  background: var(--surface-2);
  font-family: ui-monospace, 'Cascadia Mono', Consolas, monospace;
  font-size: 12px;
}
/* 示例代码块：横向可滚，不撑破页面；纵向不滚，整段都要看得见。 */
.help pre {
  margin-top: 0.6rem;
  padding: 10px 12px;
  border: 1px solid var(--border);
  border-radius: 8px;
  background: var(--surface-2);
  overflow-x: auto;
  font-family: ui-monospace, 'Cascadia Mono', Consolas, monospace;
  font-size: 12px;
  line-height: 1.6;
  white-space: pre;
}
.help a {
  color: inherit;
  border-bottom: 1px solid var(--border);
}
.help a:hover {
  border-color: var(--text-muted);
}
</style>
