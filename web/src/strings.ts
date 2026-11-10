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
    search: '搜索',
    agent: '给 AI 看',
    settings: '设置',
    about: '关于',
  },

  // 顶栏的服务状态与配色开关。
  service: {
    running: (mode: string) => `运行中（${mode}）`,
    localMode: '本机可用',
    lanMode: '局域网可用',
    stopped: '已停止',
    start: '启动',
    stop: '停止',
    copyAddress: '复制地址',
    startedToast: '已启动，AI 现在可以查了。',
    stoppedToast: '已停止，AI 现在连不上。',
    themeToDark: '换成深色',
    themeToLight: '换成浅色',
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
    saved: '已保存。',
    create: '创建',
    ok: '知道了',
  },

  // 目录选择器。它的字以前是写在组件里的，现在也收到这里。
  picker: {
    titlePick: '选择知识库目录',
    titleBrowse: '选一个文件夹',
    root: '先选一个磁盘',
    go: '前往',
    pathInputTitle: '可以点、可以改，也能把路径直接粘进来',
    pathHint: '路径可以直接粘进来，按回车跳过去。从资源管理器的地址栏复制就行。',
    pickHint:
      '这个目录下的所有 md 文件都会当成知识库的数据源。第一次用可以先建一个空目录；也可以直接把文件夹路径粘到上面。',
    emptyRoot: '先选一个磁盘，里面就是你的文件夹。',
    emptyHere: '这个位置下面没有子文件夹。直接点下面的按钮就选它自己。',
    confirmPick: '用这个文件夹',
    confirmCreate: '添加',
    newFolder: '新建文件夹',
    newFolderName: '文件夹名字',
    newFolderPlaceholder: '比如：产品文档',
    newFolderHint: '会在当前位置里建一个空文件夹，不会动任何已有的文件。',
    nameLabel: '知识库名称',
    nameHint: 'AI 能看到这个名称，并按你的描述查对应条目。',
    sharedHint:
      '这个文件夹在你自己用户目录里，只有你能读。换一个电脑用户登录后，kb-lite 读不到它，索引也就更新不了。想多人共用，把资料放到 D:\\ 这类公共位置。',
    probing: '正在数文件…',
    confirmRoot: '这个位置是磁盘根目录，整块磁盘上的 markdown 文件都会进知识库。确定要添加吗？',
    confirmEmpty: '这个目录里还没有 markdown 文件。以后往里放就行，确认要添加吗？',
    confirmCount: (n: number) => `这个目录下已经有 ${n} 个 markdown 文件，确认要添加吗？`,
    confirmCapped: (n: number) => `这个目录下已经有 ${n} 个以上 markdown 文件，确认要添加吗？`,
    defaultName: '新的知识库',
  },

  wizard: {
    title: '配置向导',

    // 第 1 屏：欢迎 + 东西放在哪
    welcome: (v: string) => `欢迎使用 KBLite v${v}`,
    welcomeHint: '这个向导会带你做完三项设置。以后随时可以重新运行它。',
    configWhere: '你的配置文件和索引都存放在这个位置：',
    configOwner: (user: string) =>
      user
        ? `这个目录只有当前用户（${user}）能访问。备份的时候把它整个复制走就行。`
        : '这个目录只有当前用户能访问。备份的时候把它整个复制走就行。',

    // 第 2 屏：端口与接口公开范围
    step2: '端口与接口公开范围',
    port: '端口',
    portHint: '默认为 7331，如果被占用会自动换一个。',
    whoCanOpen: '接口公开范围',
    loopback: '仅本设备',
    lan: '共享到局域网',
    localHint: (urls: string) => `推荐：在本设备用 ${urls} 访问。`,
    lanHint: (addr: string) => `局域网里的其他设备可以用 ${addr} 访问。`,
    lanVpn: '如果你连着 VPN，或者用了 Tailscale 这类工具，那个网络里的设备同样能访问。',
    lanPassword:
      '为了安全，会自动生成一个访问密码。它只影响给 AI 用的接口，不影响你打开这个界面。',
    lanWarn:
      '打开后，同一个网络里的人只要知道地址就能看到你的知识库。不推荐。如果你确实需要，比如想用手机上的 AI，会自动帮你打开访问密码。',

    // 第 3 屏：创建知识库
    step3: '创建知识库',
    step3Hint:
      'KBLite 的数据源是你设备上的一个文件夹。我们会把那个文件夹里的 markdown 文件录进知识库。',
    step3HintMore:
      '如果你平时用别的工具写文档，最后一步还有一个“让 AI 把文档整理成 markdown”的 skill。',
    existing: '当前已有以下知识库：',
    addDir: '添加目录',
    needKB: '还没有知识库。先添加一个，再继续。',

    // 第 4 屏：总结
    summary: '配置完成',
    summaryHint: '你的知识库已经准备好了。以后可以重新运行这个向导，也可以在设置里单独改某一项。',
    sumConfig: '配置文件位置',
    sumNetwork: '网络',
    sumLocal: (url: string) => `仅本设备：在本设备用 ${url} 访问知识库。`,
    sumLan: (addr: string) => `共享到局域网：局域网里的其他设备用 ${addr} 访问知识库。`,
    sumPassword: '访问密码',
    sumKB: '知识库',
    sumKBEmpty: '还没有添加知识库，你可以稍后自己添加。',
    sumSkill: '给 AI 用的说明书',
    sumSkillHint:
      '装好之后要重启一次 AI 工具才生效，它只在启动时读那个目录。懒得重启，就把下面那段话直接贴给它。',
    aiStuff: '给 AI 用的东西（现在不想看就收起）',
    copyCurl: '命令示例',
    copyCurlHint: '想自己试试接口时用',
    copySkill: '给 AI 的说明书',
    copySkillHint: '放在 AI 工具认识的目录里，它就会用这个知识库',
    copySkillUrl: '说明书地址',
    copySkillUrlHint:
      '你的 AI 工具里如果有“填一个网址当资料源”这种入口，就填这一行。开了访问密码的工具多半填不了，那就粘上面那一段。',
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
    actions: '操作',
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

  search: {
    title: '搜索调试',
    hint: '想知道 AI 为什么搜到这个、或者为什么搜不到，就在这里试。',
    kb: '知识库',
    query: '查询',
    placeholder: '比如：部署 回滚',
    multiWord: '多个词就都打进去，空格分开，它们会一起参与打分。',
    run: '搜索',
    empty: '这个词没有命中任何一篇文章。',
    emptyNext: '先试试更短、更常见的说法；还是不行就在知识库页点「全部更新」重新整理一遍。',
    noKB: '还没有知识库。先添加一个，再来这里试。',
    why: '为什么排在这里',
    keyword: '关键词',
    colTitle: '标题',
    colLeaf: '条目标题',
    colPath: '路径',
    colToc: '章节',
    colBody: '摘要',
    colFull: '全文',
    counts: (strong: number, weak: number, fullOnly: number) =>
      `命中要点 ${strong} · 弱命中 ${weak} · 只在全文里 ${fullOnly}`,
    approx: (n: number) => `近似命中加了 ${n} 分（中文没有分词，这里按两字一组补的）`,
    sections: (n: number) => `${n} 节`,
  },

  agent: {
    title: '给 AI 看的东西',
    hint: '下面是 AI 真正收到的原文，一个字都没改过。看不懂是正常的。',
    take: '取一次',
    retake: '重新取',
    truncated: '太长了，这里只显示前 200 KB。',
    needKB: '这一段要先选一个知识库。',
    blocks: {
      skill: 'AI 读到的说明书',
      skillHint: 'agent 第一步拿到的东西，里面有地址、密码、库列表',
      manifest: '不知道搜什么词时',
      manifestHint: '预算内裁切的分层概览',
      search: '一次检索',
      searchHint: '一个词查下去的原始结果',
      section: '一次按节阅读',
      sectionHint: 'agent 读完目录后的第二步',
    },
    query: '查询',
    path: '条目路径',
    pathPlaceholder: '比如 docs/zh/postscript-fonts.md',
    sectionNo: '第几节',
  },

  settings: {
    title: '设置',
    location: '资料放在哪',
    locationHint:
      '设置和缓存都在这个目录里。想整个搬走，复制这个目录就行；缓存（index.db）删了不影响你的资料，重新整理就有。',
    locationCopy: '复制路径',
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
    quit: '退出 kb-lite',
    quitHint: '退出后 AI 就连不上了。关掉浏览器标签不算退出。',
    paused: '服务已经暂停，AI 现在连不上。',
    rotated: '新密码已经生效，旧的立刻不能用了。',
    resume: '继续使用',
    startup: '开机自启',
    startupOn: '登录后自动跑起来，托盘会出现',
    startupOff: '关掉的话，每次开机要自己双击一次',
    startupEnabled: '已开启开机自启，下次开机会自己跑起来。',
    startupDisabled: '已关闭开机自启。',
    startupUnsupported: '这个系统还不支持开机自启，暂时只能开机后自己打开一次。',
    startupCommand: '启动的命令',
    startupMismatch: (p: string) =>
      `开机自启现在指向另一份程序：${p}。关掉再打开一次，就会指向你现在用的这个。`,
    startupFragile:
      '程序现在在下载或临时文件夹里，那里随时会被清掉。建议先把它挪到一个固定位置（比如 D:\\Tools\\KBLite），再开开机自启。',
    shortcut: '创建桌面快捷方式',
    shortcutHint: '建一个放桌面上，以后双击它就够了。',
    shortcutDone: '已创建',
    shortcutCreated: '快捷方式已经放到桌面上了。',
    update: '检查更新',
    updateCurrent: (v: string) => `当前版本 ${v}`,
    updateChecking: '正在查…',
    updateNew: (v: string) => `有新版本 ${v}`,
    updateLatest: '已经是最新的。',
    updateFailed: '这次没查到，可能没联网。稍后再试一次。',
    updateDev: '你用的是开发中的版本，正式版可能还没它新。',
    updateDownload: '去下载',
    staleTitle: 'SKILL 需要更新',
    staleBody:
      '你刚改了服务地址或访问密码，而装好的那份说明书里写的还是旧的。AI 不会自己发现这件事。',
    stalePromptHint: '把这句话发给 AI，它就会重新取一份：',
    stalePrompt: (base: string) => `kb-lite 的服务地址是 ${base}，请重新读一遍它的 SKILL 再说。`,
    staleHow:
      '想彻底更新，就去「知识库」页的「让 AI 用起来」，把带「有更新」的那几行右边的按钮点一遍。',
  },

  skill: {
    readTitle: '检索 SKILL',
    readDesc:
      '让 AI 能查这个知识库：说明书里有地址、密码，也写清了“先搜再读、一次把多个说法一起搜”的用法。',
    writeTitle: '整理 SKILL',
    writeDesc:
      '让 AI 把 docx、pdf、网页整理成能被检索的 markdown，并写进你指定的知识库目录。',

    howToAsk: '怎么跟 AI 说',
    targetAgents: '通用目录',
    targetClaude: 'Claude Code',

    stateMissing: '未安装',
    stateFresh: (t: string) => (t ? `已是最新 · ${t}` : '已是最新'),
    stateStale: '有更新',

    install: '安装',
    update: '更新',
    reinstall: '重装',
    installedToast: (path: string) => `已写入 ${path}`,

    whenStale:
      '服务地址或访问密码变了之后，这里会显示「有更新」；知识库增删改名不用管，说明书里不写库列表。',
    staleHint: '有目标落后了：服务地址或访问密码改过，点那一行右边的「更新」重装一次。',
    // Claude Code 会盯着目录（官方文档明确写了 live change detection），
    // 但它在会话启动时才知道有哪些顶层目录。所以第一次装要提一句。
    reloadHint:
      'Claude Code 会自己发现目录里的改动；如果那个目录是这次才建的，它可能要先运行 /reload-skills。其他 AI 工具不一定，保险起见重开一次会话。',

    promptReadHint: '把这句话发给 AI，它就会来查这个知识库',
    promptRead: '用 kb-lite 去我的知识库里查一下……',
    promptAuthoringHint: '把这句话发给另一个 AI，让它把文件整理成能进知识库的样子',
    promptAuthoring: '用 kb-lite-authoring 把这些文件整理进知识库「<知识库名称>」',
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
