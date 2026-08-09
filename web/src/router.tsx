import { lazy, Suspense, type ReactNode } from "react";
import { createBrowserRouter, Navigate, Outlet } from "react-router";

const RequireAuth = lazy(() => import("@/components/auth/require-auth").then((module) => ({ default: module.RequireAuth })));
const UserLayout = lazy(() => import("@/layouts/user-layout"));
const AuthScene = lazy(() => import("@/pages/auth/auth-scene").then((module) => ({ default: module.AuthScene })));
const RouteErrorPage = lazy(() => import("@/pages/route-error"));
const AdminPage = lazy(() => import("@/pages/admin"));
const AnalyticsPage = lazy(() => import("@/pages/admin/admin-route-pages").then((module) => ({ default: module.AnalyticsPage })));
const AnnouncementsPage = lazy(() => import("@/pages/admin/admin-route-pages").then((module) => ({ default: module.AnnouncementsPage })));
const CreditOperationsPage = lazy(() => import("@/pages/admin/admin-route-pages").then((module) => ({ default: module.CreditOperationsPage })));
const AccessSettingsPage = lazy(() => import("@/pages/admin/admin-route-pages").then((module) => ({ default: module.AccessSettingsPage })));
const EmailSettingsPage = lazy(() => import("@/pages/admin/admin-route-pages").then((module) => ({ default: module.EmailSettingsPage })));
const ChannelsPage = lazy(() => import("@/pages/admin/channels/channels-page"));
const LogsPage = lazy(() => import("@/pages/admin/logs/logs-page"));
const MembershipAdminPage = lazy(() => import("@/pages/admin/membership/membership-page"));
const CreditStoreAdminPage = lazy(() => import("@/pages/admin/credit-store/credit-store-page"));
const ModelPricingPage = lazy(() => import("@/pages/admin/model-pricing/model-pricing-page"));
const SuperResolutionPricingPage = lazy(() => import("@/pages/admin/super-resolution-pricing/super-resolution-pricing-page"));
const OperationsPage = lazy(() => import("@/pages/admin/operations/operations-page"));
const RedemptionCodesPage = lazy(() => import("@/pages/admin/redemption-codes/redemption-codes-page"));
const ReferralProgramPage = lazy(() => import("@/pages/admin/referrals/referral-program-page"));
const LegalSettingsPage = lazy(() => import("@/pages/admin/settings/legal-settings-page"));
const PaymentSettingsPage = lazy(() => import("@/pages/admin/settings/payment-settings-page"));
const RuntimePolicySettingsPage = lazy(() => import("@/pages/admin/settings/runtime-policy-settings-page"));
const SiteSettingsPage = lazy(() => import("@/pages/admin/settings/site-settings-page"));
const StorageSettingsPage = lazy(() => import("@/pages/admin/settings/storage-settings-page"));
const StoryboardPromptsPage = lazy(() => import("@/pages/admin/storyboard-prompts/storyboard-prompts-page"));
const UsersPage = lazy(() => import("@/pages/admin/users/users-page"));
const VoicesPage = lazy(() => import("@/pages/admin/voices/voices-page"));
const AssetsPage = lazy(() => import("@/pages/assets"));
const LoginPage = lazy(() => import("@/pages/auth/login"));
const RegisterPage = lazy(() => import("@/pages/auth/register"));
const CanvasPage = lazy(() => import("@/pages/canvas"));
const CanvasProjectPage = lazy(() => import("@/pages/canvas/project"));
const SharedCanvasPage = lazy(() => import("@/pages/canvas/shared"));
const HomePage = lazy(() => import("@/pages/home"));
const LegalDocumentPage = lazy(() => import("@/pages/legal/legal-document-page").then((module) => ({ default: module.LegalDocumentPage })));
const MembershipPage = lazy(() => import("@/pages/membership"));
const CreditStorePage = lazy(() => import("@/pages/credit-store"));
const PaymentCheckoutPage = lazy(() => import("@/pages/payment/payment-checkout-page"));
const NotFound = lazy(() => import("@/pages/not-found"));
const ProjectDetailPage = lazy(() => import("@/pages/projects/detail"));
const ProjectsPage = lazy(() => import("@/pages/projects"));
const SettingsPage = lazy(() => import("@/pages/settings"));
const SkillsPage = lazy(() => import("@/pages/skills"));
const TasksPage = lazy(() => import("@/pages/tasks"));
const TeamsPage = lazy(() => import("@/pages/teams"));
const WalletPage = lazy(() => import("@/pages/wallet"));

function RouteLoadingFallback() {
    return (
        <div className="route-loading-state" role="status" aria-live="polite">
            <span className="route-loading-label text-sm text-foreground/55">正在加载页面…</span>
        </div>
    );
}

function deferredRoute(element: ReactNode) {
    return <Suspense fallback={<RouteLoadingFallback />}>{element}</Suspense>;
}

function protectedRoute(element: ReactNode) {
    return deferredRoute(<RequireAuth>{element}</RequireAuth>);
}

export const router = createBrowserRouter([
    {
        element: deferredRoute(<AuthScene />),
        errorElement: deferredRoute(<RouteErrorPage />),
        children: [
            { path: "/login", element: deferredRoute(<LoginPage />) },
            { path: "/register", element: deferredRoute(<RegisterPage />) },
        ],
    },
    { path: "/share/canvas/:token", element: deferredRoute(<SharedCanvasPage />), errorElement: <RouteErrorPage /> },
    { path: "/pay/:token", element: deferredRoute(<PaymentCheckoutPage />), errorElement: deferredRoute(<RouteErrorPage />) },
    {
        element: deferredRoute(
            <UserLayout>
                <Outlet />
            </UserLayout>,
        ),
        errorElement: deferredRoute(<RouteErrorPage />),
        children: [
            { path: "/", element: deferredRoute(<HomePage />) },
            { path: "/legal/user-agreement", element: deferredRoute(<LegalDocumentPage document="userAgreement" />) },
            { path: "/legal/privacy-policy", element: deferredRoute(<LegalDocumentPage document="privacyPolicy" />) },
            { path: "/membership", element: deferredRoute(<MembershipPage />) },
            { path: "/credit-store", element: protectedRoute(<CreditStorePage />) },
            { path: "/tasks", element: protectedRoute(<TasksPage />) },
            { path: "/teams", element: protectedRoute(<TeamsPage />) },
            { path: "/assets", element: protectedRoute(<AssetsPage />) },
            { path: "/skills", element: protectedRoute(<SkillsPage />) },
            { path: "/wallet", element: protectedRoute(<WalletPage />) },
            { path: "/settings", element: protectedRoute(<SettingsPage />) },
            { path: "/projects", element: protectedRoute(<ProjectsPage />) },
            { path: "/projects/:projectId", element: protectedRoute(<ProjectDetailPage />) },
            { path: "/projects/:projectId/:view", element: protectedRoute(<ProjectDetailPage />) },
            { path: "/projects/:projectId/chapters/:chapterId", element: protectedRoute(<ProjectDetailPage />) },
            { path: "/canvas", element: protectedRoute(<CanvasPage />) },
            { path: "/canvas/:id", element: protectedRoute(<CanvasProjectPage />) },
            {
                path: "/admin",
                element: protectedRoute(<AdminPage />),
                children: [
                    { index: true, element: deferredRoute(<AnalyticsPage />) },
                    { path: "users", element: deferredRoute(<UsersPage />) },
                    { path: "models", element: deferredRoute(<ChannelsPage />) },
                    { path: "voices", element: deferredRoute(<VoicesPage />) },
                    { path: "model-pricing", element: deferredRoute(<ModelPricingPage />) },
                    { path: "super-resolution-pricing", element: deferredRoute(<SuperResolutionPricingPage />) },
                    { path: "storyboard-prompts", element: deferredRoute(<StoryboardPromptsPage />) },
                    { path: "announcements", element: deferredRoute(<AnnouncementsPage />) },
                    { path: "credit-operations", element: deferredRoute(<CreditOperationsPage />) },
                    { path: "redemption-codes", element: deferredRoute(<RedemptionCodesPage />) },
                    { path: "referrals", element: deferredRoute(<ReferralProgramPage />) },
                    { path: "logs", element: deferredRoute(<LogsPage />) },
                    { path: "membership", element: deferredRoute(<MembershipAdminPage />) },
                    { path: "credit-store", element: deferredRoute(<CreditStoreAdminPage />) },
                    { path: "operations", element: deferredRoute(<OperationsPage />) },
                    { path: "settings", element: <Navigate to="runtime-policy" replace /> },
                    { path: "settings/concurrency", element: <Navigate to="/admin/settings/runtime-policy" replace /> },
                    { path: "settings/runtime-policy", element: deferredRoute(<RuntimePolicySettingsPage />) },
                    { path: "settings/access", element: deferredRoute(<AccessSettingsPage />) },
                    { path: "settings/email", element: deferredRoute(<EmailSettingsPage />) },
                    { path: "settings/storage", element: deferredRoute(<StorageSettingsPage />) },
                    { path: "settings/payment", element: deferredRoute(<PaymentSettingsPage />) },
                    { path: "settings/site", element: deferredRoute(<SiteSettingsPage />) },
                    { path: "settings/legal", element: deferredRoute(<LegalSettingsPage />) },
                ],
            },
        ],
    },
    { path: "*", element: deferredRoute(<NotFound />) },
]);
