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
})
