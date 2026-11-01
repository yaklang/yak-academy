<script setup lang="ts">
import { AlertTriangle } from '@lucide/vue'
import { Toaster } from '@/components/ui/sonner'
import { AlertDialog, AlertDialogContent, AlertDialogDescription, AlertDialogFooter, AlertDialogHeader, AlertDialogTitle } from '@/components/ui/alert-dialog'
import { Button } from '@/components/ui/button'
import { confirmation, finishConfirmation } from '@/lib/notifications'
import { useTheme } from '@/lib/theme'
import 'vue-sonner/style.css'

const { resolved } = useTheme()
</script>

<template>
  <Toaster :theme="resolved" position="top-right" :close-button="true" :visible-toasts="3" />
  <AlertDialog :open="!!confirmation" @update:open="value => { if (!value) finishConfirmation(false) }">
    <AlertDialogContent class="max-h-[calc(100dvh-2rem)] w-[calc(100vw-2rem)] max-w-md overflow-y-auto">
      <AlertDialogHeader>
        <AlertTriangle class="mb-2 size-6 text-accent-foreground" />
        <AlertDialogTitle>{{ confirmation?.title }}</AlertDialogTitle>
        <AlertDialogDescription class="whitespace-pre-wrap break-words leading-relaxed">{{ confirmation?.message }}</AlertDialogDescription>
      </AlertDialogHeader>
      <AlertDialogFooter>
        <Button variant="outline" @click="finishConfirmation(false)">{{ confirmation?.options.cancelButtonText || '取消' }}</Button>
        <Button :variant="confirmation?.options.confirmButtonClass?.includes('danger') ? 'destructive' : 'default'" @click="finishConfirmation(true)">{{ confirmation?.options.confirmButtonText || '确定' }}</Button>
      </AlertDialogFooter>
    </AlertDialogContent>
  </AlertDialog>
</template>
