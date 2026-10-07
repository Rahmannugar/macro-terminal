import { BrowserRouter, Navigate, Outlet, Route, Routes } from "react-router";
import { AppShell } from "./components/app-shell";
import { AdminBoundary, AuthenticatedBoundary } from "./components/auth/authenticated-boundary";
import { PwaBanners } from "./components/pwa-banner";
import { CalendarEventsScreen } from "./routes/admin/calendar-events";
import { EntitiesScreen } from "./routes/admin/entities";
import { EntityPairsScreen } from "./routes/admin/entity-pairs";
import { IndicatorsScreen } from "./routes/admin/indicators";
import { FailedJobsScreen } from "./routes/admin/jobs";
import { KnowledgeTermsScreen } from "./routes/admin/knowledge-terms";
import { SourceConfigurationsScreen } from "./routes/admin/source-configurations";
import { SourcesScreen } from "./routes/admin/sources";
import { UsersScreen } from "./routes/admin/users";
import { ArticleRoute } from "./routes/article";
import { HomeRoute } from "./routes/home";
import { SearchRoute } from "./routes/search";
import { SignInRoute } from "./routes/sign-in";
import { SignUpRoute } from "./routes/sign-up";
import { VerifyEmailRoute } from "./routes/verify-email";

function RootLayout() {
  return (
    <AuthenticatedBoundary>
      {(account) => (
        <AppShell account={account}>
          <Outlet />
        </AppShell>
      )}
    </AuthenticatedBoundary>
  );
}

function AdminGuard() {
  return <AdminBoundary>{() => <Outlet />}</AdminBoundary>;
}

export default function App() {
  return (
    <>
      <PwaBanners />
      <BrowserRouter>
        <Routes>
          <Route path="/sign-in" element={<SignInRoute />} />
          <Route path="/sign-up" element={<SignUpRoute />} />
          <Route path="/verify-email" element={<VerifyEmailRoute />} />
          <Route element={<RootLayout />}>
            <Route path="/" element={<HomeRoute />} />
            <Route path="/search" element={<SearchRoute />} />
            <Route path="/articles/:id" element={<ArticleRoute />} />
            <Route path="/admin" element={<AdminGuard />}>
              <Route index element={<Navigate to="/admin/users" replace />} />
              <Route path="users" element={<UsersScreen />} />
              <Route path="jobs" element={<FailedJobsScreen />} />
              <Route path="entities" element={<EntitiesScreen />} />
              <Route path="entity-pairs" element={<EntityPairsScreen />} />
              <Route path="indicators" element={<IndicatorsScreen />} />
              <Route path="knowledge-terms" element={<KnowledgeTermsScreen />} />
              <Route path="sources" element={<SourcesScreen />} />
              <Route path="source-configurations" element={<SourceConfigurationsScreen />} />
              <Route path="calendar-events" element={<CalendarEventsScreen />} />
            </Route>
            <Route path="*" element={<Navigate to="/" replace />} />
          </Route>
        </Routes>
      </BrowserRouter>
    </>
  );
}
