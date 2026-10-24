// Package rules holds the user's categorisation rules and reclassifies transactions (PLAN tasks
// 15.2, 15.3): rules in position order (manual > rule > bank, the last matching rule wins, ADR 0017),
// then transfer detection between the user's own accounts (ADR 0018). A rule's match is a search and
// column filters in the table's filter language, compiled by internal/query.
package rules
