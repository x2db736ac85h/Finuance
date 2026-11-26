// The prompt gallery (Phase 27): things worth asking, in three groups. Each is a question the
// assistant can act on with the tools it has (lookups, and proposals the person applies). Words in
// [square brackets] are for the person to replace, which is why choosing a prompt fills the
// question field and sends nothing.

export type PromptGroup = { id: string; title: string; about: string; prompts: readonly Prompt[] };
export type Prompt = { title: string; text: string };

export const PROMPT_GALLERY: readonly PromptGroup[] = [
  {
    id: 'actions',
    title: 'Actions',
    about: 'Changes it proposes and you apply. Every one can be undone in Settings → Activity.',
    prompts: [
      { title: 'Add an expense', text: 'Add an expense: [24,50] at [Piekarnia Ziarno] today, paid in cash.' },
      {
        title: 'Make a rule',
        text: 'Make a rule that puts every payment to [payee] into the category [category], and apply it to the ones I already have.',
      },
      {
        title: 'Categorise a period',
        text: 'Put every uncategorised card payment from [1 to 15 March] at grocery shops into the category [Groceries].',
      },
      {
        title: 'Group a trip',
        text: 'Put everything I spent between [2 June] and [9 June] outside [my home town] into a new group called [Summer trip].',
      },
      {
        title: 'Tidy a merchant',
        text: 'Find the transactions of [merchant] that have no category and suggest one for them.',
      },
      {
        title: 'Split with a friend',
        text: 'Split the [restaurant] bill of [date] equally in the shared group [group name].',
      },
      {
        title: 'Attach a receipt',
        text: 'Here is a receipt. Find the payment it belongs to and add what was bought to its note.',
      },
    ],
  },
  {
    id: 'questions',
    title: 'Questions',
    about: 'Lookups in your own transactions. Every figure in the answer comes from one.',
    prompts: [
      {
        title: 'Where the money went',
        text: 'Where did my money go last month? Give me the five largest categories and what changed against the month before.',
      },
      {
        title: 'Subscriptions',
        text: 'Which subscriptions and regular bills do I pay, how much a month in all, and did any of them get more expensive this year?',
      },
      { title: 'One-offs', text: 'What were my ten largest one-off expenses in the last twelve months?' },
      { title: 'A weekend’s price', text: 'What does a typical weekend cost me compared with a typical weekday?' },
      {
        title: 'Small things that add up',
        text: 'Which payees do I pay small amounts very often, and what do they add up to in a year?',
      },
      { title: 'A merchant over time', text: 'How much did I spend at [merchant] each month this year?' },
      {
        title: 'Something unusual',
        text: 'Is there anything unusual in my spending of the last 30 days compared with my usual months?',
      },
    ],
  },
  {
    id: 'budget',
    title: 'Budget planning',
    about: 'Budgets and goals from what you actually spend.',
    prompts: [
      {
        title: 'Budgets from history',
        text: 'Propose monthly budgets for my six largest categories from what I spent in the last three months.',
      },
      {
        title: 'How the month is going',
        text: 'How am I doing against my budgets this month, and which ones will I overshoot at this pace?',
      },
      {
        title: 'A savings goal',
        text: 'I want to save [10 000] by [December]. Make it a goal and tell me how much that is a month.',
      },
      {
        title: 'What to cut',
        text: 'I want to spend [500] less a month. Where could that come from without touching rent and bills?',
      },
      {
        title: 'A leaner month',
        text: 'If I had to live on [4 000] next month, what would the budget per category look like?',
      },
    ],
  },
];
