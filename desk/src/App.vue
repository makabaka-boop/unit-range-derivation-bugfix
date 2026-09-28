<script setup>
import { computed, ref, watch } from 'vue'

const expression = ref('100 °C - 20 °C')
const target = ref('auto')
const rangeMode = ref(false)
const result = ref(null)
const errorInfo = ref(null)
const loading = ref(false)
const statusLine = ref('')
let reqSeq = 0

const examples = [
  '100 °C - 20 °C',
  '[20,21] °C - [68,86] °F',
  '32 °F + 9 dF',
  '1 m + 2 cm',
  '2 min * 3',
  '6 m / 3 s',
  '20 °C × 2',
  '20 °C + 300 K',
  '10 dC - 5 °C'
]

// 目标单位分组
const unitGroups = [
  {
    label: '自动 / 通用',
    units: [
      { value: 'auto', name: '自动（基准导出单位）' }
    ]
  },
  {
    label: '长度',
    units: [
      { value: 'm', name: 'm（米）' },
      { value: 'cm', name: 'cm（厘米）' }
    ]
  },
  {
    label: '质量',
    units: [
      { value: 'kg', name: 'kg（千克）' },
      { value: 'g', name: 'g（克）' }
    ]
  },
  {
    label: '时间',
    units: [
      { value: 's', name: 's（秒）' },
      { value: 'min', name: 'min（分钟）' }
    ]
  },
  {
    label: '绝对温度',
    units: [
      { value: 'K', name: 'K（开尔文）' },
      { value: 'C', name: '°C（摄氏度）' },
      { value: 'F', name: '°F（华氏度）' }
    ]
  },
  {
    label: '温差',
    units: [
      { value: 'dK', name: 'dK（开尔文温差）' },
      { value: 'dC', name: 'dC（摄氏温差）' },
      { value: 'dF', name: 'dF（华氏温差）' }
    ]
  }
]

const kindText = { normal: '普通量', delta: '温差', absolute: '绝对温度' }

const runes = computed(() => Array.from(expression.value))

// 区间入口返回的根节点带 lower/upper 字段；标量入口只有 value。
const isRangeResult = computed(() => {
  const r = result.value
  return !!r && r.root && Object.prototype.hasOwnProperty.call(r.root, 'lower')
})

// 区间上下界的“精确分数 ≈ 小数”展示
function ratText(v) {
  if (v.decimal && v.decimal !== v.exact) return `${v.exact} ≈ ${v.decimal}`
  return v.exact
}

// 高亮出错的最小区间
const highlighted = computed(() => {
  const chars = runes.value
  if (!errorInfo.value || !chars.length) return expression.value
  const { pos, end } = errorInfo.value
  const safeEnd = Math.min(Math.max(end, pos), chars.length - 1)
  const before = chars.slice(0, pos).join('')
  const middle = chars.slice(pos, safeEnd + 1).join('')
  const after = chars.slice(safeEnd + 1).join('')
  return `${before}⟦${middle}⟧${after}`
})

function splitHighlight() {
  const chars = runes.value
  if (!errorInfo.value || !chars.length) return null
  const { pos, end } = errorInfo.value
  const safePos = Math.max(0, Math.min(pos, chars.length))
  const safeEnd = Math.min(Math.max(end, safePos), chars.length - 1)
  return {
    before: chars.slice(0, safePos).join(''),
    middle: chars.slice(safePos, safeEnd + 1).join(''),
    after: chars.slice(safeEnd + 1).join('')
  }
}
const highlightParts = computed(() => splitHighlight())

async function evaluate() {
  const seq = ++reqSeq
  // 关键行为：开始一次新计算时，立刻清空上一次成功的结果与错误，
  // 让非法运算绝不可能继续展示旧结果。
  result.value = null
  errorInfo.value = null

  if (!expression.value.trim()) {
    statusLine.value = ''
    errorInfo.value = { code: 'EMPTY_EXPRESSION', message: '表达式为空', pos: 0, end: 0 }
    return
  }

  loading.value = true
  statusLine.value = '计算中…'
  try {
    const resp = await fetch(rangeMode.value ? '/api/eval-range' : '/api/eval', {
      method: 'POST',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify({
        expression: expression.value,
        target: target.value === 'auto' ? '' : target.value
      })
    })
    const data = await resp.json()
    // 过期响应直接丢弃（用户已经再次编辑）
    if (seq !== reqSeq) return
    if (resp.ok) {
      result.value = data
      statusLine.value = ''
    } else if (data && data.error) {
      errorInfo.value = data.error
      statusLine.value = ''
    } else {
      errorInfo.value = { code: 'BAD_RESPONSE', message: '服务返回了无法识别的内容', pos: 0, end: 0 }
    }
  } catch (e) {
    if (seq !== reqSeq) return
    errorInfo.value = { code: 'NETWORK', message: '无法连接计算服务：' + e.message, pos: 0, end: 0 }
    statusLine.value = ''
  } finally {
    if (seq === reqSeq) loading.value = false
  }
}

let timer = null
// 表达式开始写区间字面量（形如 [数字）时自动进入区间推导；之后不再自动
// 退出，避免编辑中途模式抖动；需要标量求值时由用户手动取消勾选。
watch([expression, target, rangeMode], () => {
  clearTimeout(timer)
  timer = setTimeout(evaluate, 280)
})

// 只在表达式实际变化时判断是否需要进入区间模式，避免 watcher 多次触发
// （目标单位、rangeMode 自身的变化）把用户手动取消勾选的操作覆盖掉。
watch(expression, val => {
  if (!rangeMode.value && /\[\s*-?(?:\d|\.)/.test(val)) {
    rangeMode.value = true
  }
})

function useExample(ex) {
  expression.value = ex
}

function formatDecimal(v) {
  if (v.decimal && v.decimal !== v.exact) return `≈ ${v.decimal}`
  return ''
}

function nodeLabel(s) {
  switch (s.nodeType) {
    case 'literal':
      return /[a-zA-Z°℃℉]/.test(s.source) ? '有理数字面量（带单位）' : '有理数字面量（无量纲）'
    case 'unary':
      return `一元 ${s.op || '-'}`
    case 'group':
      return '括号'
    case 'binary':
      return `二元 ${s.op}`
    default:
      return s.nodeType
  }
}

// 初次挂载后计算一次
evaluate()
</script>

<template>
  <div class="app">
    <header>
      <h1>单位与温度表达式 · 精确有理数推导</h1>
      <p>
        表达式含有理数字面量（整数 / 小数 / 分数）、括号、加减乘除，以及
        m·cm、kg·g、s·min、K·°C·°F 绝对温度与 dK·dC·dF 温差。
        绝对温度不能相乘除或彼此相加；每次运算都记录语法节点的种类与维度向量。
      </p>
    </header>

    <section class="panel" data-testid="editor">
      <div class="editor-row">
        <textarea
          v-model="expression"
          spellcheck="false"
          data-testid="expr-input"
          aria-label="表达式"
          @keydown.ctrl.enter="evaluate"
          @keydown.meta.enter="evaluate"
        ></textarea>
        <div class="controls">
          <select v-model="target" data-testid="target-select" aria-label="目标单位">
            <optgroup v-for="g in unitGroups" :key="g.label" :label="g.label">
              <option v-for="u in g.units" :key="u.value" :value="u.value">
                {{ u.name }}
              </option>
            </optgroup>
          </select>
          <label><input type="checkbox" v-model="rangeMode" data-testid="range-mode" />区间推导</label>
          <button class="primary" data-testid="eval-btn" @click="evaluate">
            计算（Ctrl+Enter）
          </button>
        </div>
      </div>

      <div class="chips">
        <button
          v-for="ex in examples"
          :key="ex"
          class="chip"
          type="button"
          @click="useExample(ex)"
        >
          {{ ex }}
        </button>
      </div>

      <!-- 出错时用最小区间高亮原文；成功结果区始终独立，出错即为空 -->
      <div v-if="errorInfo" class="expr-preview" data-testid="error-highlight">
        <template v-if="highlightParts">
          {{ highlightParts.before }}<mark>{{ highlightParts.middle || ' ' }}</mark
          >{{ highlightParts.after }}
        </template>
      </div>

      <div v-if="errorInfo" class="error-box" data-testid="error-box">
        <div><strong>错误码：</strong><code>{{ errorInfo.code }}</code></div>
        <div>{{ errorInfo.message }}</div>
        <div class="hint">
          定位区间（字符偏移 {{ errorInfo.pos }}–{{ errorInfo.end }}）即为出错的最小表达式区间。
        </div>
      </div>
      <div v-else class="hint">输入后自动计算；出错时上方会高亮最小出错区间。</div>
    </section>

    <!-- 成功结果：errorInfo 非空或尚未计算时整体不渲染，杜绝旧结果残留 -->
    <section v-if="result && !errorInfo" class="panel" data-testid="result">
      <div class="result-card">
        <template v-if="isRangeResult">
          <span class="value range">{{ ratText(result.root.lower) }} ~ {{ ratText(result.root.upper) }}</span>
          <span class="badge kind-range">区间结果</span>
        </template>
        <template v-else>
          <span class="value">{{ result.root.value.exact }}</span>
        </template>
        <span class="unit">{{ result.root.targetSymbol || result.root.target }}</span>
        <span v-if="!isRangeResult && formatDecimal(result.root.value)" class="decimal">
          {{ formatDecimal(result.root.value) }}
        </span>
        <span class="badge" :class="'kind-' + result.root.kind">
          {{ kindText[result.root.kind] }}
        </span>
        <span class="badge">维度：{{ result.root.dimName }}</span>
        <span class="badge">
          向量 (L, M, T, Θ) = ({{ result.root.dim.l }}, {{ result.root.dim.m }},
          {{ result.root.dim.t }}, {{ result.root.dim.q }})
        </span>
      </div>
      <div v-if="isRangeResult" class="hint">
        根节点基准值（m, kg, s, K）区间：
        <code>{{ ratText(result.root.lowerBase) }}</code> ~
        <code>{{ ratText(result.root.upperBase) }}</code>
      </div>
      <div v-else class="hint">
        根节点基准值（m, kg, s, K）：
        <code>{{ result.root.valueBase.exact }}</code>
        <span v-if="result.root.valueBase.decimal !== result.root.valueBase.exact">
          （≈ {{ result.root.valueBase.decimal }}）
        </span>
      </div>
    </section>

    <section v-if="result && !errorInfo" class="panel steps" data-testid="steps">
      <h2>逐步推导（后序：每个叶子与运算节点）</h2>
      <div
        v-for="(s, i) in result.steps"
        :key="i"
        class="step"
        :style="{ marginLeft: s.depth * 18 + 'px' }"
      >
        <div>
          <span class="step-src">{{ s.source }}</span>
          <span class="badge" :class="'kind-' + s.kind">{{ kindText[s.kind] }}</span>
          <span class="badge">节点：{{ nodeLabel(s) }}</span>
        </div>
        <div class="step-meta">
          <span class="badge">维度：{{ s.dimName }}</span>
          <span class="badge">
            ({{ s.dim.l }}, {{ s.dim.m }}, {{ s.dim.t }}, {{ s.dim.q }})
          </span>
          <span class="step-val">
            <template v-if="isRangeResult">
              基准值区间 = {{ s.lowerBase.exact
              }}<template v-if="s.lowerBase.decimal !== s.lowerBase.exact">
                ≈ {{ s.lowerBase.decimal }}</template>
              ~ {{ s.upperBase.exact
              }}<template v-if="s.upperBase.decimal !== s.upperBase.exact">
                ≈ {{ s.upperBase.decimal }}</template>
            </template>
            <template v-else>
              基准值 = {{ s.valueBase.exact
              }}<template v-if="s.valueBase.decimal !== s.valueBase.exact">
                ≈ {{ s.valueBase.decimal }}</template>
            </template>
          </span>
        </div>
        <div class="step-note">{{ s.note }}</div>
      </div>
    </section>

    <section v-if="!result && !errorInfo" class="panel">
      <div class="empty">{{ loading ? '计算中…' : statusLine || '输入表达式后这里会出现逐步推导。' }}</div>
    </section>
  </div>
</template>
