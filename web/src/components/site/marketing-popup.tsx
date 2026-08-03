import { Modal } from "antd";
import { ArrowRight, X } from "lucide-react";
import { useEffect, useMemo, useState } from "react";
import { useLocation } from "react-router";

import { siteLogoURL, useSiteSettings } from "@/components/site/site-settings-provider";
import { useUserStore } from "@/stores/use-user-store";
import { isMarketingPopupRoute, marketingPopupCampaignKey, marketingPopupStorage, recordMarketingPopupExposure, shouldShowMarketingPopup } from "./marketing-popup-policy";
import "./marketing-popup.css";

export function MarketingPopup() {
    const { settings, loading } = useSiteSettings();
    const { pathname } = useLocation();
    const user = useUserStore((state) => state.user);
    const [open, setOpen] = useState(false);
    const campaignKey = useMemo(() => (user ? marketingPopupCampaignKey(user.id, settings) : ""), [settings, user]);

    useEffect(() => {
        if (loading || !user || !isMarketingPopupRoute(pathname) || !settings.marketingPopupEnabled || !settings.marketingPopupImageUrl || !settings.marketingPopupTitle || !campaignKey) {
            setOpen(false);
            return;
        }
        const storage = marketingPopupStorage(settings.marketingPopupFrequency, window.localStorage, window.sessionStorage);
        if (!shouldShowMarketingPopup(storage, campaignKey)) return;
        const timer = window.setTimeout(() => {
            recordMarketingPopupExposure(storage, campaignKey);
            setOpen(true);
        }, 420);
        return () => window.clearTimeout(timer);
    }, [campaignKey, loading, pathname, settings.marketingPopupEnabled, settings.marketingPopupFrequency, settings.marketingPopupImageUrl, settings.marketingPopupTitle, user]);

    const close = () => {
        setOpen(false);
    };

    return (
        <Modal className="marketing-popup-modal" open={open} centered width={680} footer={null} closable={false} maskClosable onCancel={close} destroyOnHidden>
            <article className="marketing-popup-card" aria-labelledby="marketing-popup-title">
                <div className="marketing-popup-visual">
                    <img className="marketing-popup-image" src={settings.marketingPopupImageUrl} alt="" />
                    <img className="marketing-popup-logo" src={siteLogoURL(settings)} alt={settings.siteName} />
                    <button className="marketing-popup-close" type="button" aria-label="关闭活动弹窗" onClick={close}>
                        <X className="marketing-popup-close-icon" aria-hidden="true" />
                    </button>
                </div>
                <div className="marketing-popup-content">
                    <div className="marketing-popup-copy">
                        <h2 id="marketing-popup-title" className="marketing-popup-title">{settings.marketingPopupTitle}</h2>
                        {settings.marketingPopupDescription ? <p className="marketing-popup-description">{settings.marketingPopupDescription}</p> : null}
                    </div>
                    {settings.marketingPopupActionLabel && settings.marketingPopupActionUrl ? (
                        <a className="marketing-popup-action" href={settings.marketingPopupActionUrl} onClick={close}>
                            <span className="marketing-popup-action-label">{settings.marketingPopupActionLabel}</span>
                            <ArrowRight className="marketing-popup-action-icon" aria-hidden="true" />
                        </a>
                    ) : null}
                </div>
            </article>
        </Modal>
    );
}
