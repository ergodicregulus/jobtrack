/**
 * API types.
 *
 * These are ALIASES onto `api.d.ts`, which is generated from api/openapi.yaml
 * by `make generate`. They are not independent definitions: re-declaring a
 * shape here would recreate exactly the drift this indirection exists to
 * prevent — a frontend that compiles cleanly against a shape the server stopped
 * returning months ago.
 *
 * This file exists only to give the generated schemas short, readable names and
 * to hold the few view-model types that have no server counterpart. If a type
 * describes something the API sends, it belongs in the spec, not here.
 *
 * `make check-generated` is the CI gate that keeps the two in step.
 */
import type { components } from './api';

type S = components['schemas'];

export type FeedItem = S['FeedItem'];
export type FeedPage = S['FeedPage'];
export type Facets = S['Facets'];
export type Match = S['Match'];
export type Posting = S['Posting'];
export type PostingMatch = S['PostingMatch'];
export type MatchComponent = S['MatchComponent'];
export type Band = S['Band'];
export type Profile = S['Profile'];
export type ProfileUpdate = S['ProfileUpdate'];
export type Me = S['Me'];
export type Dashboard = S['Dashboard'];
export type MatchSummary = S['MatchSummary'];
export type PipelineStage = S['PipelineStage'];
export type ActionItem = S['ActionItem'];
export type DashboardJob = S['DashboardJob'];
export type MarketSummary = S['MarketSummary'];
export type Activity = S['Activity'];
export type SkillSuggestion = S['SkillSuggestion'];
export type ActivityDay = S['ActivityDay'];
export type Strength = S['Strength'];
export type StrengthItem = S['StrengthItem'];
export type TrackedItem = S['TrackedItem'];
export type TrackedUpdate = S['TrackedUpdate'];
export type ApplicationStatus = S['ApplicationStatus'];
export type WorkMode = S['WorkMode'];
export type FieldError = S['FieldError'];
export type ProblemDetail = S['ProblemDetail'];

/**
 * Presentation-only types. These have no server counterpart: the theme is a
 * client concern mirrored into a cookie, and `system` is deliberately not a
 * stored value the API resolves.
 */
export type Theme = 'system' | 'light' | 'dark';
export type Density = 'compact' | 'comfortable';

/** One line on the corpus chart: a vendor's flow and level over the window. */
export interface IngestVendorSeries {
  vendor: string;
  /** Postings first seen on each day. */
  new: number[];
  /** Postings still live at the end of each day. */
  live: number[];
}

/** The homepage corpus chart, from the source_daily rollup. See ADR-0017. */
export interface IngestSeries {
  /** Every day in the window, including quiet ones. */
  days: string[];
  series: IngestVendorSeries[];
}

/** A saved search: the feed's query string, named. See phase-5 §6.5. */
export interface SavedSearch {
  id: number;
  name: string;
  /** Replayed as /jobs?{query}. */
  query: string;
  is_default: boolean;
  created_at: string;
  last_run_at: string | null;
  /** Live postings posted since this search was last run. */
  new_since: number;
}
