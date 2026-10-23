// The overview's data (PLAN §6.2, POST /overview, budgets, investments, opening balance). One
// request per screen: every card reads the same cached document (keys.overview), so the twelve
// cards and the KPIs cost one round trip. Saving a budget, the opening balance or the investments
// refreshes it.

import { keepPreviousData, useMutation, useQuery, useQueryClient } from '@tanstack/react-query';

import { api, ApiError, data, type Schemas } from '../client';
import { withoutTransfers } from '../../table/context';
import { keys } from '../keys';
import { toast } from '../../ui/toastStore';
import type { Account } from './accounts';

export type Overview = Schemas['Overview'];
export type OverviewData = Schemas['OverviewData'];
export type Investments = Schemas['Investments'];
export type OverviewMonths = Schemas['OverviewRequest']['months'];

/** The months settings the API accepts; anything else stored in the preferences reads as 12. */
const MONTHS = [6, 12, 24, 36] as const;
export const overviewMonths = (n: number): OverviewMonths => MONTHS.find((m) => m === n) ?? 12;

export function useOverview(
  context: Schemas['QueryContext'],
  ctxKey: string,
  month: string | null,
  months: number,
  enabled = true,
) {
  const n = overviewMonths(months);
  return useQuery({
    queryKey: [...keys.overview, ctxKey, month, n],
    queryFn: async () =>
      data(
        await api.POST('/overview', {
          body: { context: withoutTransfers(context), months: n, ...(month ? { month } : {}) },
        }),
      ),
    placeholderData: keepPreviousData,
    staleTime: 30_000,
    enabled,
  });
}

/** PUT /budgets/{categoryId}: a monthly limit in minor units, or null for the default budget. */
export function useSetBudget() {
  const qc = useQueryClient();
  return useMutation({
    mutationFn: async ({ categoryId, amountMinor }: { categoryId: string; amountMinor: number | null }) => {
      data(await api.PUT('/budgets/{categoryId}', { params: { path: { categoryId } }, body: { amountMinor } }));
    },
    onSettled: () => qc.invalidateQueries({ queryKey: keys.overview }),
  });
}

/** PATCH /accounts/{id} with a new opening balance; the other fields are sent as they are. */
export function useSetOpening() {
  const qc = useQueryClient();
  return useMutation({
    mutationFn: async ({ account, minor }: { account: Account; minor: number }) =>
      data(
        await api.PATCH('/accounts/{id}', {
          params: { path: { id: account.id } },
          body: {
            name: account.name,
            currency: account.currency,
            ...(account.numberHint ? { numberHint: account.numberHint } : {}),
            archived: account.archived,
            openingBalanceMinor: minor,
          },
        }),
      ),
    onSettled: async () => {
      await qc.invalidateQueries({ queryKey: keys.accounts });
      await qc.invalidateQueries({ queryKey: keys.overview });
    },
  });
}

export const investmentsKey = ['investments'] as const;

export function useInvestments() {
  return useQuery({ queryKey: investmentsKey, queryFn: async () => data(await api.GET('/investments')) });
}

/**
 * PUT /investments. A 409 means the document changed elsewhere (another tab): StaleInvestments
 * carries the stored one, so the caller can apply its edit to it and save again.
 */
export async function saveInvestments(doc: Investments): Promise<Investments> {
  try {
    return data(await api.PUT('/investments', { body: doc }));
  } catch (e) {
    const current: unknown =
      e instanceof ApiError && e.code === 'stale_version' ? (e.problem as { current?: unknown }).current : null;
    if (current && typeof current === 'object' && 'version' in current)
      throw new StaleInvestments(current as Investments);
    throw e;
  }
}

export class StaleInvestments extends Error {
  constructor(readonly current: Investments) {
    super('stale investments');
  }
}

// Edits to the investment document apply to the cache at once (the editors show them) and are
// saved 400 ms after the last one, one request at a time; a save that answers with a new version
// keeps any edits made meanwhile.
const SAVE_DELAY = 400;
let saveTimer: ReturnType<typeof setTimeout> | undefined;
let inFlight: Promise<void> = Promise.resolve();

export function useEditInvestments() {
  const qc = useQueryClient();
  return (fn: (d: Investments) => Investments) => {
    const cur = qc.getQueryData<Investments>(investmentsKey);
    if (!cur) return;
    qc.setQueryData(investmentsKey, fn(cur));
    clearTimeout(saveTimer);
    saveTimer = setTimeout(() => {
      inFlight = inFlight.then(async () => {
        const doc = qc.getQueryData<Investments>(investmentsKey);
        if (!doc) return;
        try {
          const saved = await saveInvestments(doc);
          qc.setQueryData<Investments>(investmentsKey, (d) => (d ? { ...d, version: saved.version } : saved));
        } catch (e) {
          if (e instanceof StaleInvestments) {
            qc.setQueryData(investmentsKey, e.current);
            toast('Your investments were changed in another window — showing the saved values', { kind: 'error' });
          } else {
            await qc.invalidateQueries({ queryKey: investmentsKey });
            toast(
              e instanceof ApiError && e.status === 422
                ? `Not saved: ${e.message}`
                : 'Could not save your investments — try again',
              {
                kind: 'error',
              },
            );
          }
        }
        await qc.invalidateQueries({ queryKey: keys.overview });
      });
    }, SAVE_DELAY);
  };
}
