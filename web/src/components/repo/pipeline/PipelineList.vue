<template>
  <div class="space-y-4">
    <div v-for="group in groups" :key="group[0].id" class="space-y-2">
      <PipelineItem
        v-for="(pipeline, index) in group"
        :key="pipeline.id"
        :class="{ 'border-wp-background-400 ml-6 w-[calc(100%-1.5rem)]! border-l-2 opacity-80': index > 0 }"
        :to="{
          name: 'repo-pipeline',
          params: { pipelineId: pipeline.number },
        }"
        :pipeline="pipeline"
      />
    </div>
    <div v-if="loading" class="flex justify-center">
      <Icon name="spinner" class="animate-spin" />
    </div>
    <Panel v-else-if="pipelines?.length === 0">
      <span class="text-wp-text-100">{{ $t('repo.pipeline.no_pipelines') }}</span>
    </Panel>
    <div v-if="hasMore && !loading" class="flex justify-center">
      <Button :text="$t('repo.pipeline.load_more')" @click="$emit('loadMore')" />
    </div>
  </div>
</template>

<script lang="ts" setup>
import { computed } from 'vue';

import Button from '~/components/atomic/Button.vue';
import Icon from '~/components/atomic/Icon.vue';
import Panel from '~/components/layout/Panel.vue';
import PipelineItem from '~/components/repo/pipeline/PipelineItem.vue';
import type { Pipeline } from '~/lib/api/types';
import { groupReruns } from '~/lib/pipeline-reruns';

const props = defineProps<{
  pipelines: Pipeline[] | undefined;
  loading?: boolean;
  hasMore?: boolean;
}>();

defineEmits<{
  (event: 'loadMore'): void;
}>();

// Reruns sit under the pipeline they came from, newest run on top.
const groups = computed(() => groupReruns(props.pipelines ?? []));
</script>
