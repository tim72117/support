// 選舉版首頁的動態展示：進頁面自動播放一段固定劇本（輸入問題 → 送出 →
// 思考中 → 替身逐字回答 → 補上來源/動作卡片），播完換下一個場景，循環。
// 純前端資料，不打任何後端。劇本是下面的 SCENES 陣列，節奏由 sleep 控制；
// 離開畫面（IntersectionObserver）或分頁隱藏時暫停，避免背景空轉。
//
// 使用者偏好「減少動態效果」時不播放動畫，直接顯示第一個場景的完成畫面。

const SCENES = [
  {
    tab: '尋求支持',
    question: '請問你對社區托育的政見是什麼？',
    answer: '我主張增設公共托育據點，並放寬申請流程，讓雙薪家庭不用再排隊抽籤。完整政見可以在政見頁面查看，需要我幫你整理重點嗎？',
    cards: [
      { icon: '📜', title: '引用來源', body: '政見｜社區托育・增設公共托育據點' },
      { icon: '🤝', title: '邀請支持', body: '已詢問是否願意留下聯絡方式、加入志工' },
    ],
  },
  {
    tab: '選民服務',
    question: '我家巷口的路燈壞了快兩週，晚上很暗。',
    answer: '謝謝你回報，這確實會影響夜間安全。我已經幫你記下地點與狀況，服務團隊會盡快跟進，請問是哪一條路的巷口呢？',
    cards: [
      { icon: '📮', title: '陳情已分類', body: '類別：路燈修繕｜優先度：一般' },
      { icon: '👥', title: '轉交服務團隊', body: '已通知負責的服務處，等待跟進' },
    ],
  },
  {
    tab: '文案產生',
    question: '幫我寫一則談公共托育的臉書貼文。',
    answer: '好的，依你的政見與政績寫了一則草稿：「每個家庭都值得安心把孩子交給社區。我們將增設公共托育據點，讓照顧不再是壓力。」要再調整語氣嗎？',
    cards: [
      { icon: '✍️', title: '貼文草稿已產生', body: '依政見「社區托育」與政績 3 項整理' },
      { icon: '🌐', title: '可做成宣傳頁面', body: '一鍵產生專屬網址與 QR code' },
    ],
  },
]

const root = document.querySelector('[data-demo]')

if (root) {
  const tabsEl = root.querySelector('[data-demo-tabs]')
  const logEl = root.querySelector('[data-demo-log]')
  const inputEl = root.querySelector('[data-demo-input]')
  const sendEl = root.querySelector('[data-demo-send]')
  const reduceMotion = window.matchMedia && window.matchMedia('(prefers-reduced-motion: reduce)').matches

  const tabEls = SCENES.map((s) => {
    const t = document.createElement('span')
    t.className = 'demo-tab'
    t.textContent = s.tab
    tabsEl.appendChild(t)
    return t
  })

  let visible = true
  let runId = 0
  let visibleWaiters = []

  // 暫停時 sleep 會卡在這裡，恢復可見後才繼續。
  const waitVisible = () => (visible && !document.hidden ? Promise.resolve() : new Promise((r) => visibleWaiters.push(r)))
  const resume = () => {
    if (!visible || document.hidden) return
    const ws = visibleWaiters
    visibleWaiters = []
    ws.forEach((r) => r())
  }
  const sleep = async (ms) => {
    await new Promise((r) => setTimeout(r, ms))
    await waitVisible()
  }

  const scrollDown = () => {
    logEl.scrollTop = logEl.scrollHeight
  }

  const addBubble = (cls, text) => {
    const b = document.createElement('div')
    b.className = 'bubble ' + cls + ' demo-in'
    b.textContent = text
    logEl.appendChild(b)
    scrollDown()
    return b
  }

  const addThinking = () => {
    const b = document.createElement('div')
    b.className = 'bubble a demo-in demo-thinking'
    b.setAttribute('aria-label', '替身思考中')
    b.innerHTML = '<i></i><i></i><i></i>'
    logEl.appendChild(b)
    scrollDown()
    return b
  }

  const addCard = (c) => {
    const el = document.createElement('div')
    el.className = 'demo-card demo-in'
    const icon = document.createElement('span')
    icon.className = 'demo-card-icon'
    icon.textContent = c.icon
    const body = document.createElement('div')
    const title = document.createElement('b')
    title.textContent = c.title
    const p = document.createElement('span')
    p.textContent = c.body
    body.append(title, p)
    el.append(icon, body)
    logEl.appendChild(el)
    scrollDown()
  }

  const typeInto = async (setText, text, ms, id) => {
    for (let i = 1; i <= text.length; i++) {
      if (id !== runId) return false
      setText(text.slice(0, i))
      scrollDown()
      await sleep(ms)
    }
    return true
  }

  const setActive = (idx) => tabEls.forEach((t, i) => t.classList.toggle('active', i === idx))

  const renderStatic = (scene, idx) => {
    setActive(idx)
    logEl.textContent = ''
    addBubble('q', scene.question)
    addBubble('a', scene.answer)
    scene.cards.forEach(addCard)
  }

  const play = async (scene, idx, id) => {
    setActive(idx)
    logEl.textContent = ''
    inputEl.textContent = ''
    sendEl.classList.remove('on')

    // 1. 假打字：輸入框逐字出現，送出鈕亮起。
    sendEl.classList.add('on')
    if (!(await typeInto((t) => { inputEl.textContent = t }, scene.question, 70, id))) return
    await sleep(350)
    if (id !== runId) return

    // 2. 送出：輸入框清空，問題變成選民氣泡。
    inputEl.textContent = ''
    sendEl.classList.remove('on')
    addBubble('q', scene.question)
    await sleep(500)

    // 3. 思考中 → 替身逐字回答。
    const thinking = addThinking()
    await sleep(1100)
    if (id !== runId) return
    thinking.remove()
    const ans = addBubble('a', '')
    if (!(await typeInto((t) => { ans.textContent = t }, scene.answer, 35, id))) return
    await sleep(500)

    // 4. 補上來源／動作卡片，一張一張出現。
    for (const c of scene.cards) {
      if (id !== runId) return
      addCard(c)
      await sleep(700)
    }
    await sleep(2800)
  }

  const loop = async () => {
    const id = ++runId
    let idx = 0
    while (id === runId) {
      await play(SCENES[idx], idx, id)
      idx = (idx + 1) % SCENES.length
    }
  }

  if (reduceMotion) {
    renderStatic(SCENES[0], 0)
  } else {
    if ('IntersectionObserver' in window) {
      new IntersectionObserver((entries) => {
        visible = entries[entries.length - 1].isIntersecting
        resume()
      }).observe(root)
    }
    document.addEventListener('visibilitychange', resume)
    loop()
  }
}
