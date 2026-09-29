import { test, expect } from '@playwright/test'

// 区间推导模式的完整浏览器流程，覆盖：
//   - 减法区间端点不能整体同向取值
//   - 含负值的乘除取四个角点的最小/最大值
//   - 除数区间跨过零时必须报错、不得展示旧区间结果
//   - 摄氏/华氏绝对温度相减得到的温差区间（含目标单位换算）
//   - 每个语法节点都同时给出上下界
//   - 模式切换回标量后仍正常展示
test.describe('区间推导模式', () => {
  test.beforeEach(async ({ page }) => {
    await page.goto('/')
    await page.getByTestId('target-select').selectOption('auto')
    // 页面默认是标量模式；统一勾选区间推导。
    await page.getByTestId('range-mode').check()
  })

  test('减法区间给出完整的 [下界, 上界]，每个节点都有两个端点', async ({ page }) => {
    const input = page.getByTestId('expr-input')
    await input.fill('[1,2] m - [0,1] m')

    const result = page.getByTestId('result')
    await expect(result).toBeVisible()
    // 不能误用“下界-下界、上界-上界”，那样会得到倒置/缺失的 [1,1]。
    await expect(page.getByTestId('range-lower')).toHaveText('0')
    await expect(page.getByTestId('range-upper')).toHaveText('2')
    await expect(result).toContainText('m')

    // 三个节点（两个区间字面量 + 一个减法）都应显示基准值区间。
    const stepVals = page.locator('[data-testid="steps"] [data-testid="step-range-val"]')
    await expect(stepVals).toHaveCount(3)
    await expect(stepVals.nth(0)).toContainText('[1, 2]')
    await expect(stepVals.nth(1)).toContainText('[0, 1]')
    await expect(stepVals.nth(2)).toContainText('[0, 2]')
  })

  test('含负值的乘法按四角点取最小/最大值', async ({ page }) => {
    await page.getByTestId('expr-input').fill('[-2,-1] * [1,2]')

    await expect(page.getByTestId('result')).toBeVisible()
    await expect(page.getByTestId('range-lower')).toHaveText('-4')
    await expect(page.getByTestId('range-upper')).toHaveText('-1')

    const lastStep = page.locator('[data-testid="steps"] .step').last()
    await expect(lastStep).toContainText('[-4, -1]')
  })

  test('含负值的除法按四角点取最小/最大值', async ({ page }) => {
    await page.getByTestId('expr-input').fill('[1,2] / [-2,-1]')

    await expect(page.getByTestId('result')).toBeVisible()
    await expect(page.getByTestId('range-lower')).toHaveText('-2')
    await expect(page.getByTestId('range-upper')).toHaveText('-1/2')
  })

  test('除数区间跨过零时报错且不展示任何有限区间结果', async ({ page }) => {
    const input = page.getByTestId('expr-input')

    // 先算出一个合法区间结果。
    await input.fill('[1,2] m - [0,1] m')
    await expect(page.getByTestId('result')).toBeVisible()
    await expect(page.getByTestId('range-upper')).toHaveText('2')

    // 改成跨零除数：不得返回有限值。
    await input.fill('1 / [-1,1]')
    const errBox = page.getByTestId('error-box')
    await expect(errBox).toBeVisible()
    await expect(errBox).toContainText('DIVISOR_INTERVAL_SPANS_ZERO')
    await expect(page.getByTestId('result')).toHaveCount(0)
    await expect(page.getByTestId('steps')).toHaveCount(0)
    await expect(page.locator('body')).not.toContainText('[0, 2]')

    // 出错区间被高亮，覆盖整个除法表达式。
    await expect(page.locator('[data-testid="error-highlight"] mark')).toHaveText('1 / [-1,1]')

    // 改回合法表达式后结果恢复。
    await input.fill('[1,2] m - [0,1] m')
    await expect(page.getByTestId('result')).toBeVisible()
    await expect(page.getByTestId('range-lower')).toHaveText('0')
  })

  test('摄氏/华氏温差区间与 dC 目标换算一致', async ({ page }) => {
    const input = page.getByTestId('expr-input')
    await input.fill('[20,21] °C - [68,86] °F')
    await page.getByTestId('target-select').selectOption('dC')

    await expect(page.getByTestId('result')).toBeVisible()
    // 20°C-86°F = 293.15-303.15 = -10 dC；21°C-68°F = 294.15-293.15 = 1 dC。
    await expect(page.getByTestId('range-lower')).toHaveText('-10')
    await expect(page.getByTestId('range-upper')).toHaveText('1')
    await expect(page.locator('[data-testid="result"]')).toContainText('dC')
    await expect(page.locator('[data-testid="result"]')).toContainText('温差')
    // 基准（K）区间必须与目标单位区间一致。
    await expect(page.getByTestId('range-base')).toContainText('-10')
    await expect(page.getByTestId('range-base')).toContainText('1')

    // 切换到 dF：温差按 9/5 放大，[-10,1] dC = [-18,9/5] dF。
    await page.getByTestId('target-select').selectOption('dF')
    await expect(page.getByTestId('range-lower')).toHaveText('-18')
    await expect(page.getByTestId('range-upper')).toHaveText('9/5')
  })

  test('关闭区间模式后回到标量入口并正常展示', async ({ page }) => {
    const input = page.getByTestId('expr-input')
    await input.fill('[1,2] m - [0,1] m')
    await expect(page.getByTestId('result')).toBeVisible()

    // 同一条含区间字面量的表达式在标量入口必须被拒绝。
    await page.getByTestId('range-mode').uncheck()
    await expect(page.getByTestId('error-box')).toContainText('RANGE_REQUIRES_RANGE_EVAL')
    await expect(page.getByTestId('result')).toHaveCount(0)

    // 换成普通标量表达式，标量结果恢复单值展示。
    await input.fill('100 °C - 20 °C')
    await page.getByTestId('target-select').selectOption('dF')
    await expect(page.getByTestId('result')).toBeVisible()
    await expect(page.locator('[data-testid="result"] .value')).toHaveText('144')
    await expect(page.getByTestId('range-value')).toHaveCount(0)
    const stepVals = page.locator('[data-testid="steps"] .step .step-val')
    await expect(stepVals.first()).toContainText('基准值 =')
  })
})
