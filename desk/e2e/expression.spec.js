import { test, expect } from '@playwright/test'

// 一条完整的浏览器流程，核心断言：
// 编辑表达式 -> 看到合法结果与逐步推导 -> 改成非法表达式后，
// 非法运算不会留下旧结果（结果区必须消失，只能看到错误与高亮区间）。
test.describe('单位表达式推导', () => {
  test.beforeEach(async ({ page }) => {
    await page.goto('/')
    // 每个用例都重置为自动目标单位，避免用例间状态串扰。
    await page.getByTestId('target-select').selectOption('auto')
  })

  test('合法表达式展示精确值与每个节点的类型/维度', async ({ page }) => {
    const input = page.getByTestId('expr-input')
    await input.fill('100 °C - 20 °C')
    await page.getByTestId('target-select').selectOption('dF')

    await expect(page.getByTestId('result')).toBeVisible()
    await expect(page.locator('[data-testid="result"] .value')).toHaveText('144')
    await expect(page.locator('[data-testid="result"] .unit')).toContainText('dF')
    await expect(page.locator('[data-testid="result"]')).toContainText('温差')

    // 每个语法节点都应有类型徽标与维度向量。
    const steps = page.locator('[data-testid="steps"] .step')
    await expect(steps).toHaveCount(3) // 两个字面量 + 一个二元减法
    for (const li of await steps.all()) {
      await expect(li).toContainText('维度')
      await expect(li).toContainText(/\(\d+, \d+, \d+, \d+\)/)
    }
  })

  test('非法运算（绝对温度相乘）不会留下旧结果', async ({ page }) => {
    const input = page.getByTestId('expr-input')

    // 先算出一个合法结果。
    await input.fill('1 m + 2 cm')
    const result = page.getByTestId('result')
    await expect(result).toBeVisible()
    await expect(page.locator('[data-testid="result"] .value')).toHaveText('51/50')
    await expect(page.getByTestId('steps')).toBeVisible()

    // 改成非法表达式：20 °C × 2。
    await input.fill('20 °C × 2')

    // 错误框出现，且带有最小表达式区间的偏移量。
    const errBox = page.getByTestId('error-box')
    await expect(errBox).toBeVisible()
    await expect(errBox).toContainText('ABSOLUTE_TEMPERATURE_MULTIPLY')
    await expect(errBox).toContainText('绝对温度不能相乘')

    // 关键：旧的结果卡与逐步推导必须完全消失，页面里不能再出现 51/50。
    await expect(page.getByTestId('result')).toHaveCount(0)
    await expect(page.getByTestId('steps')).toHaveCount(0)
    await expect(page.locator('body')).not.toContainText('51/50')

    // 出错区间被 <mark> 高亮，覆盖整个乘法表达式。
    const mark = page.locator('[data-testid="error-highlight"] mark')
    await expect(mark).toHaveText('20 °C × 2')

    // 再改回合法表达式，结果应当恢复，错误框消失。
    await input.fill('6 m / 3 s')
    await expect(page.getByTestId('result')).toBeVisible()
    await expect(page.locator('[data-testid="result"] .value')).toHaveText('2')
    await expect(page.getByTestId('error-box')).toHaveCount(0)
    await expect(page.getByTestId('error-highlight')).toHaveCount(0)
  })

  test('两个绝对温度相加同样非法且不留旧结果', async ({ page }) => {
    const input = page.getByTestId('expr-input')
    await input.fill('32 °F + 9 dF')
    await expect(page.getByTestId('result')).toBeVisible()

    await input.fill('20 °C + 300 K')
    await expect(page.getByTestId('error-box')).toContainText('ABSOLUTE_TEMPERATURE_ADD')
    await expect(page.getByTestId('result')).toHaveCount(0)
  })

  test('区间模式：摄氏/华氏温差给出完整上下界与每个节点的区间推导', async ({ page }) => {
    const input = page.getByTestId('expr-input')
    const rangeBox = page.getByTestId('range-mode')

    // 输入区间字面量后自动进入区间推导模式。
    await input.fill('[20,21] °C - [68,86] °F')
    await expect(rangeBox).toBeChecked()

    const result = page.getByTestId('result')
    await expect(result).toBeVisible()
    // 基准区间 [-10,1] dK，目标自动为 dK。
    await expect(page.locator('[data-testid="result"] .value.range')).toContainText('-10')
    await expect(page.locator('[data-testid="result"] .value.range')).toContainText('~ 1')
    await expect(result).toContainText('dK')
    await expect(result).toContainText('区间结果')
    await expect(result).toContainText('温差')
    // 基准值区间提示也必须同时展示两个端点。
    await expect(result).toContainText('根节点基准值')

    // 每个节点都有下界与上界，不再只显示单个端点。
    const steps = page.locator('[data-testid="steps"] .step')
    await expect(steps).toHaveCount(3)
    for (const li of await steps.all()) {
      await expect(li).toContainText('基准值区间')
    }

    // 切到 dF 目标单位：[-18, 9/5] dF。
    await page.getByTestId('target-select').selectOption('dF')
    await expect(page.locator('[data-testid="result"] .value.range')).toContainText('-18')
    await expect(page.locator('[data-testid="result"] .value.range')).toContainText('9/5')
  })

  test('区间模式：除数区间跨零时报错并定位最小子表达式，且能恢复', async ({ page }) => {
    const input = page.getByTestId('expr-input')
    await page.getByTestId('range-mode').check()
    await input.fill('2 * (1 / [-2,2])')

    const errBox = page.getByTestId('error-box')
    await expect(errBox).toContainText('DIVISION_BY_ZERO_RANGE')
    await expect(page.getByTestId('result')).toHaveCount(0)
    await expect(page.getByTestId('steps')).toHaveCount(0)

    // 最小子表达式 1 / [-2,2] 被高亮。
    await expect(page.locator('[data-testid="error-highlight"] mark')).toHaveText('1 / [-2,2]')

    // 改回合法区间表达式后结果恢复。
    await input.fill('[10,20] m - [3,4] m')
    await expect(page.getByTestId('result')).toBeVisible()
    await expect(page.locator('[data-testid="result"] .value.range')).toContainText('6')
    await expect(page.locator('[data-testid="result"] .value.range')).toContainText('17')
    await expect(page.getByTestId('error-box')).toHaveCount(0)
  })

  test('区间与标量模式切换仍正常：返回标量表达式后标量结果卡恢复', async ({ page }) => {
    const input = page.getByTestId('expr-input')
    const rangeBox = page.getByTestId('range-mode')

    await input.fill('[2,4] * 3')
    await expect(rangeBox).toBeChecked()
    await expect(page.locator('[data-testid="result"] .value.range')).toContainText('6 ~ 12')

    // 手动取消区间模式，回到纯标量表达式：标量结果卡（单值）恢复。
    await rangeBox.uncheck()
    await input.fill('6 m / 3 s')
    await expect(page.getByTestId('result')).toBeVisible()
    await expect(page.locator('[data-testid="result"] .value')).toHaveText('2')
    await expect(page.locator('[data-testid="result"]')).toContainText('m/s')
    // 步骤恢复为单值展示。
    const step = page.locator('[data-testid="steps"] .step').first()
    await expect(step).toContainText('基准值 =')
  })
})
