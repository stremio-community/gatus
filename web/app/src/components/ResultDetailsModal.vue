<template>
  <!-- Modal backdrop -->
  <div class="fixed inset-0 bg-black/50 backdrop-blur-sm flex items-center justify-center p-4 z-50" @click="$emit('close')">
    <!-- Modal content -->
    <div class="bg-background border rounded-lg shadow-lg max-w-2xl w-full max-h-[80vh] overflow-hidden" @click.stop>
      <!-- Header -->
      <div class="flex items-center justify-between p-4 border-b">
        <div class="min-w-0">
          <h2 class="text-lg font-semibold flex items-center gap-2">
            <component :is="result.success ? CheckCircle : XCircle" :class="result.success ? 'text-green-600 dark:text-green-400' : 'text-red-600 dark:text-red-400'" class="w-5 h-5 flex-shrink-0" />
            {{ result.success ? 'Check passed' : 'Check failed' }}
          </h2>
          <p class="text-sm text-muted-foreground mt-1">
            {{ prettifyTimestamp(result.timestamp) }} • {{ formatDuration(result.duration) }}
          </p>
          <p v-if="result.hostname" class="text-sm text-muted-foreground font-mono truncate" :title="result.hostname">
            {{ result.hostname }}
          </p>
        </div>
        <Button variant="ghost" size="icon" @click="$emit('close')">
          <X class="w-4 h-4" />
        </Button>
      </div>

      <!-- Content -->
      <div class="p-4 space-y-2 overflow-y-auto max-h-[60vh]">
        <div
          v-for="entry in sortedConditionResults"
          :key="entry.index"
          class="rounded-lg border"
          :class="entry.conditionResult.success
            ? 'bg-green-50 dark:bg-green-900/30 border-green-200 dark:border-green-700'
            : 'bg-red-50 dark:bg-red-900/30 border-red-200 dark:border-red-700'"
        >
          <button
            type="button"
            class="w-full flex items-start gap-3 p-2 text-left"
            :class="{ 'cursor-default': !hasDetails(entry.conditionResult) }"
            :disabled="!hasDetails(entry.conditionResult)"
            @click="toggle(entry.index)"
          >
            <CheckCircle v-if="entry.conditionResult.success" class="w-4 h-4 mt-0.5 flex-shrink-0 text-green-600 dark:text-green-400" />
            <XCircle v-else class="w-4 h-4 mt-0.5 flex-shrink-0 text-red-600 dark:text-red-400" />
            <span class="flex-1 min-w-0 text-sm font-mono break-all"
                  :class="entry.conditionResult.success ? 'text-green-800 dark:text-green-200' : 'text-red-800 dark:text-red-200'">
              {{ entry.conditionResult.condition }}
            </span>
            <ChevronDown
              v-if="hasDetails(entry.conditionResult)"
              class="w-4 h-4 mt-0.5 flex-shrink-0 text-muted-foreground transition-transform"
              :class="{ 'rotate-180': expanded.has(entry.index) }"
            />
          </button>

          <table v-if="hasDetails(entry.conditionResult) && expanded.has(entry.index)" class="w-full text-xs border-t">
            <tbody>
              <tr v-for="(detail, detailIndex) in entry.conditionResult.details" :key="detailIndex" class="align-top">
                <th scope="row" class="py-1 pl-9 pr-3 font-medium text-left text-muted-foreground whitespace-nowrap">{{ detail.label }}</th>
                <td class="py-1 pr-3 font-mono break-all">{{ detail.value }}</td>
              </tr>
            </tbody>
          </table>
        </div>
      </div>
    </div>
  </div>
</template>

<script setup>
import { computed, ref, onMounted, onUnmounted } from 'vue'
import { X, CheckCircle, XCircle, ChevronDown } from 'lucide-vue-next'
import { Button } from '@/components/ui/button'
import { formatDuration } from '@/utils/format'
import { prettifyTimestamp } from '@/utils/time'

const props = defineProps({
  result: { type: Object, required: true }
})

const emit = defineEmits(['close'])

const hasDetails = (conditionResult) => conditionResult.details?.length > 0

// Failed conditions are what the reader came for, so they come first; the sort is stable, so pushed order is kept otherwise.
const sortedConditionResults = computed(() => {
  return (props.result.conditionResults || [])
    .map((conditionResult, index) => ({ conditionResult, index }))
    .sort((a, b) => Number(a.conditionResult.success) - Number(b.conditionResult.success))
})

const expanded = ref(new Set(
  sortedConditionResults.value
    .filter(entry => !entry.conditionResult.success && hasDetails(entry.conditionResult))
    .map(entry => entry.index)
))

const toggle = (index) => {
  const next = new Set(expanded.value)
  if (next.has(index)) {
    next.delete(index)
  } else {
    next.add(index)
  }
  expanded.value = next
}

const handleKeydown = (event) => {
  if (event.key === 'Escape') emit('close')
}

onMounted(() => window.addEventListener('keydown', handleKeydown))
onUnmounted(() => window.removeEventListener('keydown', handleKeydown))
</script>
