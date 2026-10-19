import type { Pipeline } from '~/lib/api/types';

// Folds reruns into one group per original pipeline, like CircleCI's runs of one pipeline.
// Groups keep the input order (newest first), keyed by where each group's newest run appears.
export function groupReruns(pipelines: Pipeline[]): Pipeline[][] {
  const byNumber = new Map(pipelines.map((p) => [p.number, p]));

  // Follows parents among the loaded pipelines; an unloaded parent ends the chain.
  function rootOf(pipeline: Pipeline): number {
    let current = pipeline;
    const seen = new Set<number>();
    while (current.parent > 0 && byNumber.has(current.parent) && !seen.has(current.parent)) {
      seen.add(current.number);
      current = byNumber.get(current.parent)!;
    }
    return current.number;
  }

  const groups = new Map<number, Pipeline[]>();
  for (const pipeline of pipelines) {
    const root = rootOf(pipeline);
    const group = groups.get(root) ?? [];
    group.push(pipeline);
    groups.set(root, group);
  }
  return [...groups.values()];
}

type Translate = (key: string, params: Record<string, string | number>) => string;

// How a pipeline was rerun, translated, or undefined if it is not a rerun.
export function rerunLabel(pipeline: Pipeline, t: Translate): string | undefined {
  if (!pipeline.parent) {
    return undefined;
  }
  const by = pipeline.rerun_by ?? '';
  const params = { parent: pipeline.parent, by, jobs: pipeline.rerun_mode ?? '' };
  const withBy = by !== '';
  switch (pipeline.rerun_mode ?? '') {
    case '':
      return withBy ? t('repo.pipeline.rerun_of_by', params) : t('repo.pipeline.rerun_of', params);
    case 'start':
      return withBy
        ? t('repo.pipeline.rerun_from_start_of_by', params)
        : t('repo.pipeline.rerun_from_start_of', params);
    case 'failed':
      return withBy ? t('repo.pipeline.rerun_failed_of_by', params) : t('repo.pipeline.rerun_failed_of', params);
    default:
      return withBy ? t('repo.pipeline.rerun_jobs_of_by', params) : t('repo.pipeline.rerun_jobs_of', params);
  }
}
