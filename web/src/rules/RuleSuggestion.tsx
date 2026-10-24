// The rule suggestion card under the toolbar (PLAN 15.10): after 3 manual categorisations of one
// payee group, it offers the rule that would do it ("payee contains <group>" → the category). One
// suggestion at a time; Dismiss and a created rule hide it for good (prefs.ruleHints).

import { useState } from 'react';

import { hintKey, useRuleSuggestions, type RuleSuggestion } from '../api/hooks/rules';
import { plural } from '../lib/format';
import { useUi } from '../stores/uiStore';
import { Button } from '../ui/Button';
import { Icon } from '../ui/Icon';
import { IconButton } from '../ui/IconButton';
import { RuleEditor, type RuleDraft } from './RuleEditor';

/** The rule draft a suggestion opens: its match and category, named after the payee group. */
export function suggestionDraft(s: RuleSuggestion): RuleDraft {
  return {
    name: s.payeeGroup.slice(0, 80),
    enabled: true,
    search: s.match.search ?? '',
    filters: (s.match.filters ?? []).map((f) => ({ column: f.column, cond: f.cond ?? '', excluded: f.excluded ?? [] })),
    setCategoryId: s.category.id,
    markTransfer: false,
  };
}

export function RuleSuggestionCard() {
  const list = useRuleSuggestions().data;
  const hints = useUi((s) => s.ruleHints);
  const dismiss = useUi((s) => s.dismissRuleHint);
  const [editing, setEditing] = useState<RuleSuggestion | null>(null);
  const s = list?.find((x) => !hints.includes(hintKey(x)));
  return (
    <>
      {s && !editing && (
        <div className="rule-hint" role="region" aria-label="Rule suggestion">
          <Icon name="sliders" />
          <span>
            You put {plural(s.manualCount, 'transaction')} of <b>{s.payeeGroup}</b> in <b>{s.category.name}</b> by hand.
            A rule can do it for all {plural(s.txCount, 'transaction')} and future imports.
          </span>
          <Button
            size="sm"
            variant="primary"
            onClick={() => {
              setEditing(s);
            }}
          >
            Create rule…
          </Button>
          <IconButton
            icon="x"
            size="sm"
            title="Dismiss this suggestion"
            onClick={() => {
              dismiss(hintKey(s));
            }}
          />
        </div>
      )}
      {editing && (
        <RuleEditor
          title="New rule"
          initial={suggestionDraft(editing)}
          onSaved={() => {
            dismiss(hintKey(editing));
          }}
          onClose={() => {
            setEditing(null);
          }}
        />
      )}
    </>
  );
}
