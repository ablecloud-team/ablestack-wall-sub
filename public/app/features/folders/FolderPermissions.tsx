import React, { PureComponent } from 'react';
import { connect, ConnectedProps } from 'react-redux';
import Page from 'app/core/components/Page/Page';
import { Tooltip, Icon, Button } from '@grafana/ui';
import { SlideDown } from 'app/core/components/Animations/SlideDown';
import { getNavModel } from 'app/core/selectors/navModel';
import { StoreState } from 'app/types';
import { DashboardAcl, PermissionLevel, NewDashboardAclItem } from 'app/types/acl';
import {
  getFolderByUid,
  getFolderPermissions,
  updateFolderPermission,
  removeFolderPermission,
  addFolderPermission,
} from './state/actions';
import { getLoadingNav } from './state/navModel';
import PermissionList from 'app/core/components/PermissionList/PermissionList';
import AddPermission from 'app/core/components/PermissionList/AddPermission';
import PermissionsInfo from 'app/core/components/PermissionList/PermissionsInfo';
import { GrafanaRouteComponentProps } from 'app/core/navigation/types';

export interface OwnProps extends GrafanaRouteComponentProps<{ uid: string }> {}

const mapStateToProps = (state: StoreState, props: OwnProps) => {
  const uid = props.match.params.uid;
  return {
    navModel: getNavModel(state.navIndex, `folder-permissions-${uid}`, getLoadingNav(1)),
    folderUid: uid,
    folder: state.folder,
  };
};

const mapDispatchToProps = {
  getFolderByUid,
  getFolderPermissions,
  updateFolderPermission,
  removeFolderPermission,
  addFolderPermission,
};

const connector = connect(mapStateToProps, mapDispatchToProps);

export type Props = OwnProps & ConnectedProps<typeof connector>;

export interface State {
  isAdding: boolean;
}

export class FolderPermissions extends PureComponent<Props, State> {
  constructor(props: Props) {
    super(props);

    this.state = {
      isAdding: false,
    };
  }

  componentDidMount() {
    this.props.getFolderByUid(this.props.folderUid);
    this.props.getFolderPermissions(this.props.folderUid);
  }

  onOpenAddPermissions = () => {
    this.setState({ isAdding: true });
  };

  onRemoveItem = (item: DashboardAcl) => {
    this.props.removeFolderPermission(item);
  };

  onPermissionChanged = (item: DashboardAcl, level: PermissionLevel) => {
    this.props.updateFolderPermission(item, level);
  };

  onAddPermission = (newItem: NewDashboardAclItem) => {
    return this.props.addFolderPermission(newItem);
  };

  onCancelAddPermission = () => {
    this.setState({ isAdding: false });
  };

  render() {
    const { navModel, folder } = this.props;
    const { isAdding } = this.state;

    if (folder.id === 0) {
      return (
        <Page navModel={navModel}>
          <Page.Contents isLoading={true}>
            <span />
          </Page.Contents>
        </Page>
      );
    }

    const folderInfo = { title: folder.title, url: folder.url, id: folder.id };

    return (
      <Page navModel={navModel}>
        <Page.Contents>
          <div className="page-action-bar">
            <h3 className="page-sub-heading">폴더 권한</h3>
            <Tooltip placement="auto" content={<PermissionsInfo />}>
              <Icon className="icon--has-hover page-sub-heading-icon" name="question-circle" />
            </Tooltip>
            <div className="page-action-bar__spacer" />
            <Button className="pull-right" onClick={this.onOpenAddPermissions} disabled={isAdding}>
              권한 추가
            </Button>
          </div>
          <SlideDown in={isAdding}>
            <AddPermission onAddPermission={this.onAddPermission} onCancel={this.onCancelAddPermission} />
          </SlideDown>
          <PermissionList
            items={folder.permissions}
            onRemoveItem={this.onRemoveItem}
            onPermissionChanged={this.onPermissionChanged}
            isFetching={false}
            folderInfo={folderInfo}
          />
        </Page.Contents>
      </Page>
    );
  }
}

export default connector(FolderPermissions);
