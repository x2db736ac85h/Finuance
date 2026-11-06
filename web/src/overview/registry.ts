// The overview's cards in the legacy OV_DEFS order, with Goals (Phase 20) after the budgets row (the
// grid places them by their class, see app.css "overview").

import type { ChartDef } from '../charts/types';
import { balanceChart } from './Balance';
import { budgetsChart } from './Budgets';
import { cashFlowChart } from './CashFlow';
import { highlightsChart } from './Highlights';
import { goalsChart } from './Goals';
import { fireChart, investmentChart, yieldChart } from './Investments';
import { sankeyChart } from './Sankey';
import { savingsChart } from './Savings';
import { largestChart, recurringChart } from './Tables';
import { weekdayChart } from './Weekday';

export const OV_CHARTS: readonly ChartDef[] = [
  highlightsChart,
  cashFlowChart,
  budgetsChart,
  recurringChart,
  goalsChart,
  fireChart,
  yieldChart,
  investmentChart,
  sankeyChart,
  largestChart,
  savingsChart,
  weekdayChart,
  balanceChart,
];

export const ovChartOf = (k: string): ChartDef | undefined => OV_CHARTS.find((c) => c.key === k);
