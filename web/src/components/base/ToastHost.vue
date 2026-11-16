<script setup lang="ts">
import type { Component } from "vue";
import { CircleCheckFilled, CircleCloseFilled, InfoFilled, WarningFilled } from "@element-plus/icons-vue";
import { useToasts } from "@/composables/useToast";

const { toasts, remove } = useToasts();

const icons: Record<string, Component> = {
  success: CircleCheckFilled,
  error: CircleCloseFilled,
  info: InfoFilled,
  warning: WarningFilled,
};
</script>

<template>
  <Teleport to="body">
    <div class="toast-host">
      <TransitionGroup name="toast">
        <div
          v-for="t in toasts"
          :key="t.id"
          class="toast"
          :class="`toast--${t.kind}`"
          @click="remove(t.id)"
        >
          <span class="toast__icon"><component :is="icons[t.kind]" /></span>
          <span class="toast__msg">{{ t.message }}</span>
        </div>
      </TransitionGroup>
    </div>
  </Teleport>
</template>

<style scoped>
.toast-host {
  position: fixed;
  top: 20px;
  right: 20px;
  z-index: var(--z-toast);
  display: flex;
  flex-direction: column;
  gap: 10px;
}
.toast {
  display: flex;
  align-items: center;
  gap: 10px;
  min-width: 240px;
  max-width: 380px;
  padding: 12px 16px;
  background: var(--surface);
  border-radius: var(--radius-sm);
  box-shadow: var(--shadow-pop);
  border-left: 4px solid var(--text-muted);
  cursor: pointer;
}
.toast--success {
  border-left-color: var(--success);
}
.toast--error {
  border-left-color: var(--danger);
}
.toast--info {
  border-left-color: var(--info);
}
.toast--warning {
  border-left-color: var(--warning);
}
.toast__icon {
  width: 20px;
  height: 20px;
  display: grid;
  place-items: center;
  border-radius: 50%;
  color: #fff;
  background: var(--text-muted);
  flex-shrink: 0;
}
.toast__icon svg {
  width: 14px;
  height: 14px;
}
.toast--success .toast__icon {
  background: var(--success);
}
.toast--error .toast__icon {
  background: var(--danger);
}
.toast--info .toast__icon {
  background: var(--info);
}
.toast--warning .toast__icon {
  background: var(--warning);
}
.toast__msg {
  font-size: 13px;
  color: var(--text);
}

.toast-enter-active,
.toast-leave-active {
  transition: all 0.25s ease;
}
.toast-enter-from,
.toast-leave-to {
  opacity: 0;
  transform: translateX(20px);
}
</style>
