<script setup lang="ts">
import { Check, Close } from "@element-plus/icons-vue";
defineProps<{ confirmTitle?: string; cancelTitle?: string }>();
defineEmits<{ confirm: []; cancel: [] }>();
</script>

<template>
  <button
    type="button"
    class="folder-inline-btn confirm"
    :title="confirmTitle"
    @mousedown.prevent
    @click="$emit('confirm')"
  >
    <Check />
  </button>
  <button
    type="button"
    class="folder-inline-btn cancel"
    :title="cancelTitle"
    @mousedown.prevent
    @click="$emit('cancel')"
  >
    <Close />
  </button>
</template>
