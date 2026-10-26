// The activity log (PLAN 17.6): GET /me/activity, newest first, a page at a time.

import { useInfiniteQuery } from '@tanstack/react-query';

import { api, data, type Schemas } from '../client';
import { keys } from '../keys';

export type ActivityEvent = Schemas['ActivityEvent'];
export type ActivityAction = ActivityEvent['action'];

export const ACTIVITY_PAGE = 50;

export function useActivity() {
  return useInfiniteQuery({
    queryKey: keys.activity,
    queryFn: async ({ pageParam }) =>
      data(
        await api.GET('/me/activity', {
          params: { query: { limit: ACTIVITY_PAGE, ...(pageParam ? { before: pageParam } : {}) } },
        }),
      ),
    initialPageParam: 0,
    getNextPageParam: (last) => last.nextBefore,
  });
}
