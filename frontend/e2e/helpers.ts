import type { Page } from '@playwright/test'
import { E2E } from './fixtures/consts'

// 经真实登录页登录（登录旅程本身就是被测行为，不走 API 直设 token）。
export async function login(page: Page, who: { username: string; password: string }) {
  await page.goto('/login')
  await page.getByPlaceholder('用户名').fill(who.username)
  await page.getByPlaceholder('密码').fill(who.password)
  await page.getByRole('button', { name: '登录' }).click()
  await page.waitForURL((u) => !u.pathname.startsWith('/login'))
}

export function loginAsAdmin(page: Page) {
  return login(page, E2E.admin)
}

// 可传覆盖密码：账号生命周期用例改密/重置后以新密码重登。
export function loginAsStudent(page: Page, password: string = E2E.student.password) {
  return login(page, { username: E2E.student.username, password })
}

// 登出按钮位于两端布局头部；登出后应回到登录页。
export async function logout(page: Page) {
  await page.getByRole('button', { name: '登出' }).click()
  await page.waitForURL((u) => u.pathname === '/login')
}
