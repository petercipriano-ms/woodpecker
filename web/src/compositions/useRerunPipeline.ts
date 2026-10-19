import { useI18n } from 'vue-i18n';
import { useRouter } from 'vue-router';

import useApiClient from '~/compositions/useApiClient';
import { useAsyncAction } from '~/compositions/useAsyncAction';
import { requiredInject } from '~/compositions/useInjectProvide';
import useNotifications from '~/compositions/useNotifications';

// rerun: undefined restarts everything, 'failed' reruns what did not succeed, a workflow name reruns that workflow;
// the server also reruns whatever depends on the rerun workflows.
export function useRerunPipeline() {
  const apiClient = useApiClient();
  const router = useRouter();
  const notifications = useNotifications();
  const i18n = useI18n();
  const repo = requiredInject('repo');

  const { doSubmit: rerunPipeline, isLoading: isRerunning } = useAsyncAction(
    async (pipelineNumber: number | string, rerun?: string) => {
      let newPipeline;
      try {
        newPipeline = await apiClient.restartPipeline(repo.value.id, String(pipelineNumber), { fork: true, rerun });
      } catch (error) {
        const reason = error instanceof Error ? error.message : String(error);
        notifications.notify({ title: i18n.t('repo.pipeline.actions.rerun_error', { error: reason }), type: 'error' });
        return;
      }
      notifications.notify({ title: i18n.t('repo.pipeline.actions.restart_success'), type: 'success' });
      await router.push({ name: 'repo-pipeline', params: { pipelineId: newPipeline.number } });
    },
  );

  return { rerunPipeline, isRerunning };
}
