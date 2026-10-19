import { describe, expect, it } from 'vitest';

import type { Pipeline } from '~/lib/api/types';
import { groupReruns, rerunLabel } from '~/lib/pipeline-reruns';

function pipeline(number: number, parent = 0): Pipeline {
  return { id: number, number, parent } as Pipeline;
}

function numbers(groups: Pipeline[][]): number[][] {
  return groups.map((group) => group.map((p) => p.number));
}

describe('groupReruns', () => {
  it('folds a chain of reruns under its original pipeline, newest first', () => {
    const list = [pipeline(20, 19), pipeline(19, 18), pipeline(18), pipeline(17)];
    expect(numbers(groupReruns(list))).toEqual([[20, 19, 18], [17]]);
  });

  it('keeps two reruns of the same pipeline together', () => {
    const list = [pipeline(12, 10), pipeline(11), pipeline(10, 0), pipeline(9, 0)];
    expect(numbers(groupReruns(list))).toEqual([[12, 10], [11], [9]]);
  });

  it('starts a group at a rerun whose parent is not loaded', () => {
    const list = [pipeline(31, 30), pipeline(29)];
    expect(numbers(groupReruns(list))).toEqual([[31], [29]]);
  });

  it('survives a parent loop', () => {
    const list = [pipeline(2, 1), pipeline(1, 2)];
    expect(groupReruns(list).flat()).toHaveLength(2);
  });
});

describe('rerunLabel', () => {
  const t = (key: string, params: Record<string, string | number>) => `${key} ${JSON.stringify(params)}`;

  it('is empty for a pipeline that is not a rerun', () => {
    expect(rerunLabel(pipeline(5), t)).toBeUndefined();
  });

  it('names the mode and who started it', () => {
    const rerun = { ...pipeline(19, 18), rerun_mode: 'failed', rerun_by: 'peter' } as Pipeline;
    expect(rerunLabel(rerun, t)).toBe('repo.pipeline.rerun_failed_of_by {"parent":18,"by":"peter","jobs":"failed"}');
  });

  it('lists the jobs of a specific-job rerun', () => {
    const rerun = { ...pipeline(21, 20), rerun_mode: 'pr-env' } as Pipeline;
    expect(rerunLabel(rerun, t)).toMatch(/^repo\.pipeline\.rerun_jobs_of /);
  });

  it('falls back for reruns made before the mode was recorded', () => {
    expect(rerunLabel(pipeline(20, 19), t)).toMatch(/^repo\.pipeline\.rerun_of /);
  });
});
