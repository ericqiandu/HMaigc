import { App, Button, Modal } from "antd";
import { useState } from "react";

import { useAssetStore } from "@/stores/use-asset-store";
import { useCanvasUiStore } from "@/stores/canvas/use-canvas-ui-store";
import { deleteCanvasProjectsWithRemoteSync } from "@/services/user-data-sync";
import type { CanvasProjectDeletionResult } from "@/services/canvas-project-deletion";

type CanvasDeleteProjectsDialogProps = {
    deleteAction?: (ids: string[]) => Promise<CanvasProjectDeletionResult>;
};

export function CanvasDeleteProjectsDialog({ deleteAction = deleteCanvasProjectsWithRemoteSync }: CanvasDeleteProjectsDialogProps = {}) {
    const { message } = App.useApp();
    const ids = useCanvasUiStore((state) => state.deleteProjectIds);
    const setDeleteIds = useCanvasUiStore((state) => state.setDeleteProjectIds);
    const removeSelectedIds = useCanvasUiStore((state) => state.removeSelectedProjectIds);
    const cleanupImages = useAssetStore((state) => state.cleanupImages);
    const [deleting, setDeleting] = useState(false);
    const close = () => {
        if (!deleting) setDeleteIds([]);
    };
    const confirm = async () => {
        if (deleting) return;
        setDeleting(true);
        try {
            const result = await deleteAction(ids);
            if (result.deletedIds.length > 0) {
                cleanupImages();
                removeSelectedIds(result.deletedIds);
            }
            if (result.failures.length === 0) {
                setDeleteIds([]);
                return;
            }
            setDeleteIds(result.failures.map((failure) => failure.id));
            const detail = result.failures.length === 1 ? result.failures[0]?.reason : `${result.failures.length} 个画布删除失败`;
            message.error(detail || "画布删除失败");
        } finally {
            setDeleting(false);
        }
    };

    return (
        <Modal
            rootClassName="canvas-overlay-modal canvas-overlay-confirm"
            title="删除画布？"
            open={ids.length > 0}
            centered
            onCancel={close}
            closable={!deleting}
            keyboard={!deleting}
            mask={{ closable: !deleting }}
            footer={
                <>
                    <Button disabled={deleting} onClick={close}>
                        取消
                    </Button>
                    <Button danger type="primary" loading={deleting} onClick={() => void confirm()}>
                        删除
                    </Button>
                </>
            }
        >
            <p className="text-sm text-stone-500">将删除 {ids.length} 个画布，里面的节点和连线也会一起移除。</p>
        </Modal>
    );
}
