import { createBrowserRouter, Navigate, Outlet } from "react-router";

import { RequireAuth } from "@/components/auth/require-auth";
import UserLayout from "@/layouts/user-layout";
import AdminPage from "@/pages/admin";
import { AccessSettingsPage, AnalyticsPage, AnnouncementsPage, CreditOperationsPage, EmailSettingsPage } from "@/pages/admin/admin-route-pages";
import ChannelsPage from "@/pages/admin/channels/channels-page";
import LogsPage from "@/pages/admin/logs/logs-page";
import MembershipAdminPage from "@/pages/admin/membership/membership-page";
import ModelPricingPage from "@/pages/admin/model-pricing/model-pricing-page";
import OperationsPage from "@/pages/admin/operations/operations-page";
import RedemptionCodesPage from "@/pages/admin/redemption-codes/redemption-codes-page";
import ReferralProgramPage from "@/pages/admin/referrals/referral-program-page";
import RuntimePolicySettingsPage from "@/pages/admin/settings/runtime-policy-settings-page";
import StorageSettingsPage from "@/pages/admin/settings/storage-settings-page";
import PaymentSettingsPage from "@/pages/admin/settings/payment-settings-page";
import SiteSettingsPage from "@/pages/admin/settings/site-settings-page";
import LegalSettingsPage from "@/pages/admin/settings/legal-settings-page";
import StoryboardPromptsPage from "@/pages/admin/storyboard-prompts/storyboard-prompts-page";
import UsersPage from "@/pages/admin/users/users-page";
import VoicesPage from "@/pages/admin/voices/voices-page";
import AssetsPage from "@/pages/assets";
import { AuthScene } from "@/pages/auth/auth-scene";
import LoginPage from "@/pages/auth/login";
import RegisterPage from "@/pages/auth/register";
import CanvasPage from "@/pages/canvas";
import CanvasProjectPage from "@/pages/canvas/project";
import SharedCanvasPage from "@/pages/canvas/shared";
import HomePage from "@/pages/home";
import { LegalDocumentPage } from "@/pages/legal/legal-document-page";
import MembershipPage from "@/pages/membership";
import NotFound from "@/pages/not-found";
import RouteErrorPage from "@/pages/route-error";
import SkillsPage from "@/pages/skills";
import TasksPage from "@/pages/tasks";
import TeamsPage from "@/pages/teams";
import WalletPage from "@/pages/wallet";
import ProjectsPage from "@/pages/projects";
import ProjectDetailPage from "@/pages/projects/detail";
import SettingsPage from "@/pages/settings";

export const router = createBrowserRouter([
    {
        element: <AuthScene />,
        errorElement: <RouteErrorPage />,
        children: [
            { path: "/login", element: <LoginPage /> },
            { path: "/register", element: <RegisterPage /> },
        ],
    },
    { path: "/share/canvas/:token", element: <SharedCanvasPage />, errorElement: <RouteErrorPage /> },
    {
        element: (
            <UserLayout>
                <Outlet />
            </UserLayout>
        ),
        errorElement: <RouteErrorPage />,
        children: [
            { path: "/", element: <HomePage /> },
            { path: "/legal/user-agreement", element: <LegalDocumentPage document="userAgreement" /> },
            { path: "/legal/privacy-policy", element: <LegalDocumentPage document="privacyPolicy" /> },
            { path: "/membership", element: <MembershipPage /> },
            { path: "/tasks", element: <RequireAuth><TasksPage /></RequireAuth> },
            { path: "/teams", element: <RequireAuth><TeamsPage /></RequireAuth> },
            { path: "/assets", element: <RequireAuth><AssetsPage /></RequireAuth> },
            { path: "/skills", element: <RequireAuth><SkillsPage /></RequireAuth> },
            { path: "/wallet", element: <RequireAuth><WalletPage /></RequireAuth> },
            { path: "/settings", element: <RequireAuth><SettingsPage /></RequireAuth> },
            { path: "/projects", element: <RequireAuth><ProjectsPage /></RequireAuth> },
            { path: "/projects/:projectId", element: <RequireAuth><ProjectDetailPage /></RequireAuth> },
            { path: "/projects/:projectId/:view", element: <RequireAuth><ProjectDetailPage /></RequireAuth> },
            { path: "/projects/:projectId/chapters/:chapterId", element: <RequireAuth><ProjectDetailPage /></RequireAuth> },
            { path: "/canvas", element: <RequireAuth><CanvasPage /></RequireAuth> },
            { path: "/canvas/:id", element: <RequireAuth><CanvasProjectPage /></RequireAuth> },
            {
                path: "/admin",
                element: <RequireAuth><AdminPage /></RequireAuth>,
                children: [
                    { index: true, element: <AnalyticsPage /> },
                    { path: "users", element: <UsersPage /> },
                    { path: "models", element: <ChannelsPage /> },
                    { path: "voices", element: <VoicesPage /> },
                    { path: "model-pricing", element: <ModelPricingPage /> },
                    { path: "storyboard-prompts", element: <StoryboardPromptsPage /> },
                    { path: "announcements", element: <AnnouncementsPage /> },
                    { path: "credit-operations", element: <CreditOperationsPage /> },
                    { path: "redemption-codes", element: <RedemptionCodesPage /> },
                    { path: "referrals", element: <ReferralProgramPage /> },
                    { path: "logs", element: <LogsPage /> },
                    { path: "membership", element: <MembershipAdminPage /> },
                    { path: "operations", element: <OperationsPage /> },
                    { path: "settings", element: <Navigate to="runtime-policy" replace /> },
                    { path: "settings/concurrency", element: <Navigate to="/admin/settings/runtime-policy" replace /> },
                    { path: "settings/runtime-policy", element: <RuntimePolicySettingsPage /> },
                    { path: "settings/access", element: <AccessSettingsPage /> },
                    { path: "settings/email", element: <EmailSettingsPage /> },
                    { path: "settings/storage", element: <StorageSettingsPage /> },
                    { path: "settings/payment", element: <PaymentSettingsPage /> },
                    { path: "settings/site", element: <SiteSettingsPage /> },
                    { path: "settings/legal", element: <LegalSettingsPage /> },
                ],
            },
        ],
    },
    { path: "*", element: <NotFound /> },
]);
