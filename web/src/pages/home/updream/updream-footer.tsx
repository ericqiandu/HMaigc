export function UpdreamFooter() {
    return (
        <footer className="updream-footer pb-10 text-center text-[12px] leading-7 text-white/35">
            <p className="updream-footer-legal-links">
                <button type="button" className="updream-footer-link transition-colors hover:text-white/70">用户协议</button>
                <span className="updream-footer-divider mx-3 text-white/20">|</span>
                <button type="button" className="updream-footer-link transition-colors hover:text-white/70">隐私政策</button>
                <span className="updream-footer-divider mx-3 text-white/20">|</span>
                <button type="button" className="updream-footer-link transition-colors hover:text-white/70">特别鸣谢</button>
            </p>
            <p className="updream-footer-compliance">
                <button type="button" className="updream-footer-link transition-colors hover:text-white/70">
                    知识产权及用户合规声明
                </button>
            </p>
            <p className="updream-footer-registration">
                <a
                    href="https://beian.miit.gov.cn/"
                    target="_blank"
                    rel="noreferrer"
                    className="updream-footer-link transition-colors hover:text-white/70"
                >
                    蜀ICP备2025153849号
                </a>
            </p>
        </footer>
    );
}
