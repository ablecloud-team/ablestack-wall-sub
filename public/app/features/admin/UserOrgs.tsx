import React, { PureComponent, ReactElement } from 'react';
import { css, cx } from '@emotion/css';
import {
  Button,
  ConfirmButton,
  Field,
  HorizontalGroup,
  Icon,
  Modal,
  stylesFactory,
  Themeable,
  Tooltip,
  useStyles2,
  withTheme,
} from '@grafana/ui';
import { GrafanaTheme, GrafanaTheme2 } from '@grafana/data';
import { AccessControlAction, Organization, OrgRole, UserOrg } from 'app/types';
import { OrgPicker, OrgSelectItem } from 'app/core/components/Select/OrgPicker';
import { OrgRolePicker } from './OrgRolePicker';
import { contextSrv } from 'app/core/core';

interface Props {
  orgs: UserOrg[];
  isExternalUser?: boolean;

  onOrgRemove: (orgId: number) => void;
  onOrgRoleChange: (orgId: number, newRole: OrgRole) => void;
  onOrgAdd: (orgId: number, role: OrgRole) => void;
}

interface State {
  showAddOrgModal: boolean;
}

export class UserOrgs extends PureComponent<Props, State> {
  state = {
    showAddOrgModal: false,
  };

  showOrgAddModal = (show: boolean) => () => {
    this.setState({ showAddOrgModal: show });
  };

  render() {
    const { orgs, isExternalUser, onOrgRoleChange, onOrgRemove, onOrgAdd } = this.props;
    const { showAddOrgModal } = this.state;
    const addToOrgContainerClass = css`
      margin-top: 0.8rem;
    `;
    const canAddToOrg = contextSrv.hasPermission(AccessControlAction.OrgUsersAdd);
    return (
      <>
        <h3 className="page-heading">조직</h3>
        <div className="gf-form-group">
          <div className="gf-form">
            <table className="filter-table form-inline">
              <tbody>
                {orgs.map((org, index) => (
                  <OrgRow
                    key={`${org.orgId}-${index}`}
                    isExternalUser={isExternalUser}
                    org={org}
                    onOrgRoleChange={onOrgRoleChange}
                    onOrgRemove={onOrgRemove}
                  />
                ))}
              </tbody>
            </table>
          </div>
          <div className={addToOrgContainerClass}>
            {canAddToOrg && (
              <Button variant="secondary" onClick={this.showOrgAddModal(true)}>
                조직에 사용자 추가
              </Button>
            )}
          </div>
          <AddToOrgModal isOpen={showAddOrgModal} onOrgAdd={onOrgAdd} onDismiss={this.showOrgAddModal(false)} />
        </div>
      </>
    );
  }
}

const getOrgRowStyles = stylesFactory((theme: GrafanaTheme) => {
  return {
    removeButton: css`
      margin-right: 0.6rem;
      text-decoration: underline;
      color: ${theme.palette.blue95};
    `,
    label: css`
      font-weight: 500;
    `,
    disabledTooltip: css`
      display: flex;
    `,
    tooltipItem: css`
      margin-left: 5px;
    `,
    tooltipItemLink: css`
      color: ${theme.palette.blue95};
    `,
  };
});

interface OrgRowProps extends Themeable {
  org: UserOrg;
  isExternalUser?: boolean;
  onOrgRemove: (orgId: number) => void;
  onOrgRoleChange: (orgId: number, newRole: OrgRole) => void;
}

interface OrgRowState {
  currentRole: OrgRole;
  isChangingRole: boolean;
}

class UnThemedOrgRow extends PureComponent<OrgRowProps, OrgRowState> {
  state = {
    currentRole: this.props.org.role,
    isChangingRole: false,
  };

  onOrgRemove = () => {
    const { org } = this.props;
    this.props.onOrgRemove(org.orgId);
  };

  onChangeRoleClick = () => {
    const { org } = this.props;
    this.setState({ isChangingRole: true, currentRole: org.role });
  };

  onOrgRoleChange = (newRole: OrgRole) => {
    this.setState({ currentRole: newRole });
  };

  onOrgRoleSave = () => {
    this.props.onOrgRoleChange(this.props.org.orgId, this.state.currentRole);
  };

  onCancelClick = () => {
    this.setState({ isChangingRole: false });
  };

  render() {
    const { org, isExternalUser, theme } = this.props;
    const { currentRole, isChangingRole } = this.state;
    const styles = getOrgRowStyles(theme);
    const labelClass = cx('width-16', styles.label);
    const canChangeRole = contextSrv.hasPermission(AccessControlAction.OrgUsersRoleUpdate);
    const canRemoveFromOrg = contextSrv.hasPermission(AccessControlAction.OrgUsersRemove);

    return (
      <tr>
        <td className={labelClass}>{org.name}</td>
        {isChangingRole ? (
          <td>
            <OrgRolePicker value={currentRole} onChange={this.onOrgRoleChange} />
          </td>
        ) : (
          <td className="width-25">{org.role}</td>
        )}
        <td colSpan={1}>
          <div className="pull-right">
            {canChangeRole && (
              <ChangeOrgButton
                isExternalUser={isExternalUser}
                onChangeRoleClick={this.onChangeRoleClick}
                onCancelClick={this.onCancelClick}
                onOrgRoleSave={this.onOrgRoleSave}
              />
            )}
          </div>
        </td>
        <td colSpan={1}>
          <div className="pull-right">
            {canRemoveFromOrg && (
              <ConfirmButton
                confirmText="제거 확인"
                confirmVariant="destructive"
                onCancel={this.onCancelClick}
                onConfirm={this.onOrgRemove}
              >
                조직에서 제거
              </ConfirmButton>
            )}
          </div>
        </td>
      </tr>
    );
  }
}

const OrgRow = withTheme(UnThemedOrgRow);

const getAddToOrgModalStyles = stylesFactory(() => ({
  modal: css`
    width: 500px;
  `,
  buttonRow: css`
    text-align: center;
  `,
  modalContent: css`
    overflow: visible;
  `,
}));

interface AddToOrgModalProps {
  isOpen: boolean;
  onOrgAdd(orgId: number, role: string): void;
  onDismiss?(): void;
}

interface AddToOrgModalState {
  selectedOrg: Organization | null;
  role: OrgRole;
}

export class AddToOrgModal extends PureComponent<AddToOrgModalProps, AddToOrgModalState> {
  state: AddToOrgModalState = {
    selectedOrg: null,
    role: OrgRole.Admin,
  };

  onOrgSelect = (org: OrgSelectItem) => {
    this.setState({ selectedOrg: org.value! });
  };

  onOrgRoleChange = (newRole: OrgRole) => {
    this.setState({
      role: newRole,
    });
  };

  onAddUserToOrg = () => {
    const { selectedOrg, role } = this.state;
    this.props.onOrgAdd(selectedOrg!.id, role);
  };

  onCancel = () => {
    if (this.props.onDismiss) {
      this.props.onDismiss();
    }
  };

  render() {
    const { isOpen } = this.props;
    const { role } = this.state;
    const styles = getAddToOrgModalStyles();
    return (
      <Modal
        className={styles.modal}
        contentClassName={styles.modalContent}
        title="조직에 추가"
        isOpen={isOpen}
        onDismiss={this.onCancel}
      >
        <Field label="조직">
          <OrgPicker onSelected={this.onOrgSelect} />
        </Field>
        <Field label="권한">
          <OrgRolePicker value={role} onChange={this.onOrgRoleChange} />
        </Field>
        <Modal.ButtonRow>
          <HorizontalGroup spacing="md" justify="center">
            <Button variant="secondary" fill="outline" onClick={this.onCancel}>
              취소
            </Button>
            <Button variant="primary" onClick={this.onAddUserToOrg}>
              조직에 추가
            </Button>
          </HorizontalGroup>
        </Modal.ButtonRow>
      </Modal>
    );
  }
}

interface ChangeOrgButtonProps {
  isExternalUser?: boolean;
  onChangeRoleClick: () => void;
  onCancelClick: () => void;
  onOrgRoleSave: () => void;
}

const getChangeOrgButtonTheme = (theme: GrafanaTheme2) => ({
  disabledTooltip: css`
    display: flex;
  `,
  tooltipItemLink: css`
    color: ${theme.v1.palette.blue95};
  `,
});

export function ChangeOrgButton({
  onChangeRoleClick,
  isExternalUser,
  onOrgRoleSave,
  onCancelClick,
}: ChangeOrgButtonProps): ReactElement {
  const styles = useStyles2(getChangeOrgButtonTheme);
  return (
    <div className={styles.disabledTooltip}>
      <ConfirmButton
        confirmText="Save"
        onClick={onChangeRoleClick}
        onCancel={onCancelClick}
        onConfirm={onOrgRoleSave}
        disabled={isExternalUser}
      >
        Change role
      </ConfirmButton>
      {isExternalUser && (
        <Tooltip
          placement="right-end"
          content={
            <div>
              This user&apos;s role is not editable because it is synchronized from your auth provider. Refer to
              the&nbsp;
              <a
                className={styles.tooltipItemLink}
                href={'https://grafana.com/docs/grafana/latest/auth'}
                rel="noreferrer"
                target="_blank"
              >
                Grafana authentication docs
              </a>
              &nbsp;for details.
            </div>
          }
        >
          <Icon name="question-circle" />
        </Tooltip>
      )}
    </div>
  );
}
