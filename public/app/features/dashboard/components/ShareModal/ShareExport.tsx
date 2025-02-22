import React, { PureComponent } from 'react';
import { saveAs } from 'file-saver';
import { getBackendSrv } from 'app/core/services/backend_srv';
import { Button, Field, Modal, Switch } from '@grafana/ui';
import { DashboardExporter } from 'app/features/dashboard/components/DashExportModal';
import { appEvents } from 'app/core/core';
import { ShowModalReactEvent } from 'app/types/events';
import { ViewJsonModal } from './ViewJsonModal';
import { config } from '@grafana/runtime';
import { ShareModalTabProps } from './types';

interface Props extends ShareModalTabProps {}

interface State {
  shareExternally: boolean;
  trimDefaults: boolean;
}

export class ShareExport extends PureComponent<Props, State> {
  private exporter: DashboardExporter;

  constructor(props: Props) {
    super(props);
    this.state = {
      shareExternally: false,
      trimDefaults: false,
    };

    this.exporter = new DashboardExporter();
  }

  onShareExternallyChange = () => {
    this.setState({
      shareExternally: !this.state.shareExternally,
    });
  };

  onTrimDefaultsChange = () => {
    this.setState({
      trimDefaults: !this.state.trimDefaults,
    });
  };

  onSaveAsFile = () => {
    const { dashboard } = this.props;
    const { shareExternally } = this.state;
    const { trimDefaults } = this.state;

    if (shareExternally) {
      this.exporter.makeExportable(dashboard).then((dashboardJson: any) => {
        if (trimDefaults) {
          getBackendSrv()
            .post('/api/dashboards/trim', { dashboard: dashboardJson })
            .then((resp: any) => {
              this.openSaveAsDialog(resp.dashboard);
            });
        } else {
          this.openSaveAsDialog(dashboardJson);
        }
      });
    } else {
      if (trimDefaults) {
        getBackendSrv()
          .post('/api/dashboards/trim', { dashboard: dashboard.getSaveModelClone() })
          .then((resp: any) => {
            this.openSaveAsDialog(resp.dashboard);
          });
      } else {
        this.openSaveAsDialog(dashboard.getSaveModelClone());
      }
    }
  };

  onViewJson = () => {
    const { dashboard } = this.props;
    const { shareExternally } = this.state;
    const { trimDefaults } = this.state;

    if (shareExternally) {
      this.exporter.makeExportable(dashboard).then((dashboardJson: any) => {
        if (trimDefaults) {
          getBackendSrv()
            .post('/api/dashboards/trim', { dashboard: dashboardJson })
            .then((resp: any) => {
              this.openJsonModal(resp.dashboard);
            });
        } else {
          this.openJsonModal(dashboardJson);
        }
      });
    } else {
      if (trimDefaults) {
        getBackendSrv()
          .post('/api/dashboards/trim', { dashboard: dashboard.getSaveModelClone() })
          .then((resp: any) => {
            this.openJsonModal(resp.dashboard);
          });
      } else {
        this.openJsonModal(dashboard.getSaveModelClone());
      }
    }
  };

  openSaveAsDialog = (dash: any) => {
    const dashboardJsonPretty = JSON.stringify(dash, null, 2);
    const blob = new Blob([dashboardJsonPretty], {
      type: 'application/json;charset=utf-8',
    });
    const time = new Date().getTime();
    saveAs(blob, `${dash.title}-${time}.json`);
  };

  openJsonModal = (clone: object) => {
    appEvents.publish(
      new ShowModalReactEvent({
        props: {
          json: JSON.stringify(clone, null, 2),
        },
        component: ViewJsonModal,
      })
    );

    this.props.onDismiss?.();
  };

  render() {
    const { onDismiss } = this.props;
    const { shareExternally } = this.state;
    const { trimDefaults } = this.state;

    return (
      <>
        <p className="share-modal-info-text">이 대시보드를 내보냅니다.</p>
        <Field label="Export for sharing externally">
          <Switch id="share-externally-toggle" value={shareExternally} onChange={this.onShareExternallyChange} />
        </Field>
        {config.featureToggles.trimDefaults && (
          <Field label="Export with default values removed">
            <Switch id="trim-defaults-toggle" value={trimDefaults} onChange={this.onTrimDefaultsChange} />
          </Field>
        )}
        <Modal.ButtonRow>
          <Button variant="secondary" onClick={onDismiss} fill="outline">
            취소
          </Button>
          <Button variant="secondary" onClick={this.onViewJson}>
            JSON 보기
          </Button>
          <Button variant="primary" onClick={this.onSaveAsFile}>
            파일로 저장
          </Button>
        </Modal.ButtonRow>
      </>
    );
  }
}
