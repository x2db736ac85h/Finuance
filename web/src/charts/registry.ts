// The pane's charts in their default order (legacy CARD_DEFS). Overview charts join in Phase 12.

import { cashflowChart } from './CashflowChart';
import { categoriesChart } from './CategoriesChart';
import { dailyChart } from './DailyHeatmap';
import { paceChart } from './PaceChart';
import { payeesChart } from './PayeesChart';
import { summaryChart } from './SummaryChart';
import type { ChartDef, ChartKey } from './types';

export const CHARTS: readonly ChartDef[] = [
  summaryChart,
  categoriesChart,
  cashflowChart,
  paceChart,
  payeesChart,
  dailyChart,
];

export const chartOf = (k: string): ChartDef | undefined => CHARTS.find((c) => c.key === k);

export const isChartKey = (k: string): k is ChartKey => CHARTS.some((c) => c.key === k);
