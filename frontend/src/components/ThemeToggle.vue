<script setup lang="ts">
import { Laptop, Moon, Sun } from '@lucide/vue'
import { Button } from '@/components/ui/button'
import { DropdownMenu, DropdownMenuContent, DropdownMenuLabel, DropdownMenuRadioGroup, DropdownMenuRadioItem, DropdownMenuSeparator, DropdownMenuTrigger } from '@/components/ui/dropdown-menu'
import { useTheme } from '@/lib/theme'

const { preference, resolved, setTheme } = useTheme()
</script>

<template>
  <DropdownMenu>
    <DropdownMenuTrigger as-child>
      <Button variant="ghost" size="icon" aria-label="切换主题" :title="resolved === 'dark' ? '当前为深色模式' : '当前为浅色模式'">
        <Moon v-if="resolved === 'dark'" class="size-4" />
        <Sun v-else class="size-4" />
      </Button>
    </DropdownMenuTrigger>
    <DropdownMenuContent align="end" class="min-w-40">
      <DropdownMenuLabel>外观</DropdownMenuLabel>
      <DropdownMenuSeparator />
      <DropdownMenuRadioGroup :model-value="preference" @update:model-value="value => setTheme(String(value))">
        <DropdownMenuRadioItem value="light"><Sun class="mr-2 size-4" />浅色模式</DropdownMenuRadioItem>
        <DropdownMenuRadioItem value="dark"><Moon class="mr-2 size-4" />深色模式</DropdownMenuRadioItem>
        <DropdownMenuRadioItem value="system"><Laptop class="mr-2 size-4" />跟随系统</DropdownMenuRadioItem>
      </DropdownMenuRadioGroup>
    </DropdownMenuContent>
  </DropdownMenu>
</template>
