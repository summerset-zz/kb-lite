// 所有界面文案集中在这里（docs/06 第八节：术语一处都不许散）。
//
// 规则：读者假定为“只会用 AI、不懂电脑”。术语对照表里禁用的词不要出现：
// 不写“索引/切片/token/局域网/笔记”，写“整理/访问密码/同一个 WiFi”。

export const s = {
  brand: 'KBLite',
  brandOwner: 'Summerset-zz',

  /** 凡是给 agent 用的内容块上方都放这一句（docs/06 §8.5）。 */
  agentNote: '这一段是给 AI 用的，你不用看懂。点“复制”把它粘给 AI 就行。',

  nav: {
    kbs: '知识库',
    settings: '设置',
    about: '关于',
  },

  common: {
    save: '保存',
    cancel: '取消',
    retry: '再试一次',
    back: '返回',
    next: '下一步',
    start: '开始使用',
    loading: '正在加载…',
    copy: '复制',
    copied: '已复制',
    close: '关闭',
  },

  wizard: {
    title: '先做三件事',
    step1: '记录文件放在哪',
    step1Hint: '这里只存索引缓存。删掉它不会丢失你的知识库文件，重建一下就好。',
    defaultLocation: '默认位置',
    customLocation: '换个位置',
    step2: '端口与访问范围',
    port: '端口',
    portHint: '端口一般不用改。被别的软件占用时会自动换一个。',
    whoCanOpen: '谁可以打开',
    loopback: '只有这台电脑',
    lan: '同一个 WiFi 下的其他设备也能打开',
    lanWarn:
      '打开后，同一个网络里的人只要知道地址就能看到你的知识库。不推荐。如果你确实需要，比如想用手机上的 AI，会自动帮你打开访问密码。',
    step3: '第一个知识库',
    pickFolder: '选一个放着资料的文件夹',
    pickFolderHint: '我们会把它整理成 AI 能查的形式。文件不会被改动。',
    name: '名称',
    done: '准备好了',
    doneHint: '下面这几段都是给 AI 用的，你不用看懂。想用的时候点“复制”粘给它。',
    copyCurl: '命令示例',
    copyCurlHint: '想自己试试接口时用',
    copySkill: '给 AI 的说明书',
    copySkillHint: '放在 AI 工具认识的目录里，它就会用这个知识库',
    copyMcp: 'MCP 配置',
    copyMcpHint: '存成 .mcp.json，支持 MCP 的工具会认它',
    copyAuthoring: '整理说明',
    copyAuthoringHint: '交给另一个 AI，让它把 docx、pdf 这些整理成能进知识库的格式',
  },

  kbs: {
    empty: '还没有添加知识库。',
    emptyHint: '选一个放着资料的文件夹，我们会把它整理成 AI 能查的形式。文件不会被改动。',
    add: '添加知识库',
    addedAt: '上次整理',
    never: '还没整理过',
    pending: '待更新',
    entries: '篇',
    sections: '节',
    skipped: '个文件没被收录',
    enable: '启用',
    disable: '关闭',
    update: '更新',
    updateAll: '全部更新',
    rename: '改名',
    remove: '移除',
    indexing: '正在整理',
    indexingHint: '整理期间 AI 查到的是上一次的结果。',
    ready: '已就绪',
    disabled: '已关闭',
    failed: '这次没整理成',
  },

  settings: {
    title: '设置',
    dbPath: '记录文件位置',
    dbPathHint: '它只是个索引缓存，换位置会在新位置重新整理。',
    port: '端口',
    whoCanOpen: '谁可以打开',
    password: '访问密码',
    passwordOn: '开启',
    passwordOff: '关闭',
    passwordOnHint: '把下面这段话发给你的 AI，或填进它所在工具的配置里。少了它，AI 会连不上。',
    passwordOffHint:
      '关掉之后，这台电脑上的任何程序都能读你的知识库。你浏览器里打开的网页不行，它们打不开这个服务。',
    rotate: '换一个密码',
    rerunWizard: '重新运行向导',
    restartNeeded: '记录文件位置要重启 kb-lite 之后才生效。',
    moved: '端口已经换好了，这个页面用的是旧地址。',
    moveNow: '跳到新地址',
    theme: '配色',
    themeAuto: '跟随系统',
    themeLight: '浅色',
    themeDark: '深色',
    quit: '退出 kb-lite',
    quitHint: '退出后 AI 就连不上了。关掉浏览器标签不算退出。',
    service: '服务',
    paused: '服务已经暂停，AI 现在连不上。',
    resume: '继续使用',
    pause: '暂停服务',
    pauseHint: '暂停只是让 AI 连不上，索引和进程都还在。点“继续使用”立刻恢复，不用重新整理。',
  },

  skill: {
    installRead: '安装检索 SKILL',
    installReadHint: '让 AI 能查这个知识库',
    installAuthoring: '安装整理 SKILL',
    installAuthoringHint: '让 AI 把你的文件整理进知识库',
    // 这句话必须说准：我们只能把文件写进去，能不能认到是 AI 工具自己的事。
    afterInstall:
      '装好后要重启一次 AI 工具才生效：它只在启动时读那个目录。不想重启，就把下面这段话直接贴给它，这一次也能用。',
    installed: '已安装到',
    promptReadHint: '把这句话发给 AI，它就会来查这个知识库',
    promptRead: '用 kb-lite 去我的知识库里查一下……',
    promptAuthoringHint: '把这句话发给另一个 AI，让它把文件整理成能进知识库的样子',
    promptAuthoring: '用 kb-lite-authoring 把这些文件整理成可以进知识库的格式',
  },

  about: {
    title: '关于作者',
    bio: 'Hybrid Humanoid including features below: - product manager - designer - front-end coder - musician - fox',
    email: '邮箱',
    github: 'GitHub',
    support: '支持作者',
    supportHint: '扫这个码请作者喝杯咖啡。',
    feedback: '反馈问题',
    checkUpdate: '检查更新',
    soon: '之后再做',
  },

  footer: {
    author: '作者 Summerset-zz',
    feedback: '反馈问题',
    checkUpdate: '检查更新',
    support: '支持作者',
  },

  errors: {
    daemonDown: '连不上服务。它可能已经退出了。',
    generic: '出错了',
  },}
